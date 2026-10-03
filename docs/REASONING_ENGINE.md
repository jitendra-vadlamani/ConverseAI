# Reasoning engine

ConverseAI answers every message with a single tool-calling agent ([`internal/agent`](../internal/agent)) that runs as a background *run* ([`internal/service/run_service.go`](../internal/service/run_service.go)).

## One turn, step by step

1. **Store the message.** The user's text and attachment ids are saved exactly as typed. File contents and retrieved passages are rebuilt at request time and never stored in the conversation.
2. **Prepare files.**
   - Every text or PDF attachment in the conversation is indexed in Chroma once: idempotent, keyed by file id, with about 400-token chunks in cosine space.
   - Files attached to this turn go into the prompt whole if they are small (≤ 24 KB of text); larger ones get a preview.
   - Images are attached to the message. If the chosen model has no vision, the turn is routed to `DEFAULT_VISION_MODEL`.
3. **Pre-retrieve.** If the conversation has documents, the top passages for the question are retrieved. Ranking is hybrid: vector search, then BM25 re-ranking fused with reciprocal rank fusion. The passages are added as numbered sources.
4. **Fit the context.** If the prompt would pass 80% of `num_ctx`, earlier turns are summarized into the conversation summary. If it's still too large, this turn's file text is trimmed.
5. **Agent loop** (up to 5 tool rounds):
   - The model streams its reply with the available tools offered (native Ollama tool calling).
   - If it calls tools, each call's arguments are validated against the tool's JSON Schema, the tool runs, and its output goes back as a `tool` message. Invalid arguments, unknown tools and repeated identical calls are answered with an explanatory error instead of running.
   - After each round the state is saved to the run document, which is what makes runs resumable.
   - When the model answers without tool calls, or the round limit is reached, the loop ends.
6. **Save.** The answer gets a footer listing the sources it cited, and the run records:
   - tokens
   - the models used
   - the prompt version
   - the model parameters

## Tools

| Tool | Offered when | What it does |
| --- | --- | --- |
| `web_search` | always | DuckDuckGo (Wikipedia fallback). Fetches the top 4 pages through the SSRF-safe client and ranks passages by relevance (0.6), authority (0.2) and freshness (0.2). Passages from different pages that are about the same thing (embedding similarity > 0.85) are checked for contradiction by the model; contradicting ones are flagged and their score halved. |
| `search_documents` | the conversation has text files | Hybrid retrieval, limited to this conversation's files |
| `extract_text_from_image` | the conversation has images | OCR with `DEFAULT_OCR_MODEL`, only on images attached to this conversation |
| `translate` | a translation model is configured | Translation with `DEFAULT_TRANSLATION_MODEL` |

## Grounding and prompt-injection defence

- **Numbered sources:** every retrieved passage is numbered, and the system prompt requires `[n]` citations that refer only to those numbers.
- **Fencing:** web pages, file text and tool output are wrapped in `<untrusted_data>` tags. The system prompt says that content is data and its instructions must not be followed. Closing tags inside the content are neutralised so it can't break out of the fence.
- **Versioned prompts:** prompts live in [`internal/agent/prompts`](../internal/agent/prompts), and `PromptVersion` is recorded on every run.

## Visibility

Every step emits an event to the conversation's **System Logs** tab: retrieval, tool decisions and arguments, search and extraction results with scores, summarization, model routing and run start or finish. Events are encrypted at rest and expire after 30 days.

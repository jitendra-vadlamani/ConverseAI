# ConverseAI production gap plan

Oct 3, 2026

Audit of the ConverseAI repo (branch main, commit 218874a) against production readiness, using [Agentic AI: Zero to Hero](https://claude.ai/code/artifact/d620c078-4216-4982-9a53-bfce34e04dce) as the checklist. Live, editable version: [ConverseAI production gap plan](https://claude.ai/code/artifact/4493083a-acae-40e9-91fd-d9f7a0a481e1).

## Verdict

ConverseAI is not deployable today: `main` at 218874a does not compile, and once it does, any logged-in user can write into and watch another user's conversations. Fix the build and the 6 critical defects first (Phase 0–1, about 2 weeks for one engineer), then add the evals, tracing, guardrails and tests that the Zero to Hero sections 10, 11, 15 and 17 treat as table stakes.

What was checked: all 33 Go files (~5,000 lines), the React client's API layer, Dockerfile, docker-compose, deploy.sh and .env.example. `go build ./...` was run in a `golang:1.25-alpine` container; it stops at an import cycle before reaching the other compile errors listed below. There are 0 test files in the repo.

## Status (Oct 3, 2026, branch `production-readiness`)

All 33 bugs (B1–B33) are fixed, and every phase's checklist was worked through. Where the result differs from the plan, the table below says so.

**How it was verified:**
- `go build`, `go vet`, staticcheck, `gofmt` and the client's `tsc`, ESLint and Vite build all pass.
- 47 unit tests and 6 end-to-end tests (real MongoDB, MinIO and Chroma; fake model) pass under `-race`.
- A smoke test of the built binary against a stub Ollama HTTP server, with real web search, worked end to end.
- The production image builds and the compose stack comes up healthy.
- A backup → destroy all volumes → restore drill passed twice from empty volumes.

No real model was used: the remote Ollama server was down.

| Gate / item | Result |
| --- | --- |
| Phase 0: build passes in CI on every PR | Done. `.github/workflows/ci.yml` runs build, vet, staticcheck, unit and integration tests, client lint and build, and the Docker build. Smoke test = `internal/e2e`. |
| Phase 1: second account can't read, write or subscribe; refuses default secrets | Done; covered by `TestChatFlowAndIsolation` and `config` tests. |
| Phase 2: 20-turn conversation, no duplicate vectors, no leaks under `-race` | Ingestion is idempotent per file (tested: no re-embedding on later turns). There's no dedicated 20-turn test, and it hasn't been run with real models. |
| Phase 3: native tools, schema validation, retry, fencing, versioned prompts, sampling params | Done. The keyword router is replaced by the agent deciding whether to call tools. Routing: vision fallback, OCR tool, translation tool; the coding model is user-selected (the README no longer claims auto-routing). |
| Phase 3: hybrid retrieval + reranker, token-aware chunking | **Partial.** BM25 re-ranks the vector top-30 with RRF (not corpus-wide BM25), and there is no cross-encoder reranker (Ollama has no rerank API). Chunking is sentence-aware and sized by a word-based token estimate, not a tokenizer. |
| Phase 3: planner at temperature 0 | **Changed.** There is no separate planner any more. Fact-check, OCR and other internal calls use temperature 0; the chat model uses its catalog temperature. |
| Phase 4: golden set, metrics, CI gate | 50-case set, `cmd/eval` (tool accuracy, correctness, injection resistance, citation rate, optional LLM-judged faithfulness) and `baseline.json` are written, but **never run against a real model**. The baseline numbers are placeholders. The gate runs via `evals.yml` on a self-hosted runner, not on every PR (hosted CI has no GPU). Ragas/DeepEval weren't used. |
| Phase 4: tracing, metrics, slog | slog JSON logs, Prometheus `/metrics`, and OpenTelemetry spans (run, LLM call, tool) exported when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. Langfuse or Phoenix itself isn't deployed. |
| Phase 4: red team | Prompt-injection cases are in the golden set (canary strings); promptfoo/garak weren't used. |
| Phase 4: feedback capture | 👍/👎 with optional correction in the UI, stored encrypted; `converseai export-feedback` emits golden-set candidates. |
| Phase 5: restore drill; restart loses no work | Done. Drill passed; `TestCancelAndResumeAfterRestart` resumes a run in a new server instance. |
| Phase 5: queue + persisted step state | **Changed.** Runs are persisted in MongoDB with leases and heartbeats, which serves as the durable queue; Redis Streams/NATS weren't used. Redis pub/sub is an optional broker for multiple instances. Rate limits are still per instance. |
| Phase 5: TLS, non-root, pinned images, health checks | Done; the Caddy profile hasn't been tested with a real certificate. `minio/minio` is no longer on Docker Hub, so compose pins the `pgsty/minio` community build. Chroma has no auth of its own and relies on the internal network. |
| Phase 5: key rotation, account deletion, indexes, list without bodies, docs | Done. |

**Behaviour changes to know about:**
- MongoDB now requires authentication.
- Session cookies are `Secure` by default.
- Register still returns 409 for a taken email, as planned, so emails remain enumerable through sign-up.

## Bugs found

33 defects, 6 of them critical. Paths are relative to the repo root; line numbers are at commit 218874a. Rows marked "verify" depend on a runtime version I could not run here.

| ID | Bug | Where | Impact | Severity |
| --- | --- | --- | --- | --- |
| B1 | Import cycle: `service` imports `orchestrator`, `orchestrator/executor.go` imports `service` | `internal/orchestrator/executor.go:21` | `go build` fails; nothing ships | Critical |
| B2 | Half-finished refactor: `planner.go:360-444` sits outside any function; `Execute` has no return or closing brace; `emitEvent` defined twice; `orchestrator.go` uses `api.ChatRequest`, `strings`, `ollama` without imports; `chat_service.go:447` uses undefined `convID`/`uID`; `RagService` lacks `Ingest`/`Search`; `SystemLLMRepository` lacks `GetDefaultModel` | `internal/orchestrator/*`, `internal/service/*` | Compile errors behind B1 | Critical |
| B3 | Orchestrator calls `Execute(ctx, step.Tool, step.Reason, images)`; the interface takes tasks or a `PlanStep`. The tool's JSON input is never passed | `orchestrator.go:83`, `executor.go:31` | Agent tools get no arguments | Critical |
| B4 | `StreamCompletion` never checks that the conversation belongs to the caller | `chat_handler.go:121`, `chat_service.go:196` | Any user can write into another user's chat and have the model replay its history | Critical |
| B5 | `StreamEvents` subscribes to any conversation id without an ownership check | `chat_handler.go:477` | Live leak of other users' prompts and tool output | Critical |
| B6 | Compose passes `JWT_SECRET=${JWT_SECRET}`, which is `""` when unset; `LookupEnv` accepts it, so tokens are signed with an empty key and no warning fires. `DB_ENCRYPTION_KEY` is never passed by compose or `deploy.sh`, so the public default key encrypts production data | `config.go:42,55`, `docker-compose.yml`, `deploy.sh` | Forgeable sessions; at-rest encryption is decorative | Critical |
| B7 | Same empty-string override for `EMBEDDING_MODEL` and every `DEFAULT_*_MODEL` | `docker-compose.yml` | Empty model names; chat and RAG fail at runtime | High |
| B8 | Completion goroutine keeps writing to `ResponseWriter` after the handler returns on client disconnect | `chat_handler.go:199-221` | Data race, possible panic | High |
| B9 | Ollama `http.Client{Timeout: 120s}` covers the whole streamed body; scanner error ignored | `ollama.go:76`, `chat_service.go:513-531` | Answers longer than 2 min are cut and saved as complete | High |
| B10 | `KeepAlive: 0` with `omitempty` drops `keep_alive`, so "unload" loads the old model again | `ollama.go:41,188` | One-model-in-VRAM feature does nothing | High |
| B11 | `num_ctx` = full context window (131,072 for gemma4, 262,144 for qwen3-vl) | `system_models.json`, `chat_service.go:508` | KV cache exceeds consumer GPU memory; OOM or CPU fallback | High |
| B12 | `prepareMessages` re-resolves every past attachment each turn, re-downloading files and spawning a new RAG ingest per >20 KB file per turn | `chat_service.go:429,543` | Duplicate vectors, CPU/GPU load grows with chat length | High |
| B13 | The stored user message is `finalPrompt` (file text + RAG chunks), not what the user typed | `chat_service.go:247` | Conversation doc grows toward Mongo's 16 MB limit; injected text persists in history | High |
| B14 | `user_message_received` and planner events store raw prompts and plans in plaintext | `chat_service.go:238,277` | Bypasses the AES-256-GCM field encryption | High |
| B15 | Presigned URLs are signed for `converseai-storage:9000`, an internal Docker hostname | `storage.go:379` | File preview and download fail in the browser | High |
| B16 | Deltas with `\n` are written raw into `data:` lines; the client drops continuation lines and resets the event type per read chunk | `chat_handler.go:201-205`, `client/src/api/chat.ts:184-212` | Markdown newlines vanish; thoughts can render as the answer | High |
| B17 | Cookie `Secure: false`, no `SameSite`; handlers accept any content type | `auth_handler.go:79` | Cross-site form POSTs can hit state-changing endpoints (CSRF) | High |
| B18 | `FetchPageContent` fetches any URL from search results, follows redirects, and `io.ReadAll`s an unbounded body | `search_service.go:481` | SSRF into the Docker network; memory exhaustion | High |
| B19 | Chroma calls use `/api/v1` against `chromadb/chroma:latest`; non-2xx ignored on add/delete; `Search` returns `nil, nil` on any error (verify against your Chroma version) | `rag_service.go` | RAG can fail silently on current Chroma | High |
| B20 | Mongo, MinIO (default `admin/password123`) and Chroma ports published on the host with no auth | `docker-compose.yml`, `deploy.sh` | Anyone on the network reads every conversation and file | High |
| B21 | Files tab GETs `/api/chat/conversations/files`, but that route only accepts DELETE (`ListConversationFiles` is never routed) | `main.go:103`, `client/src/api/chat.ts:103` | Files tab is always empty | Medium |
| B22 | `queryChroma` builds fresh `Evidence`, so authority and freshness are 0; conflict flags are set on cluster copies and never reach the ranked list | `rag_service.go:254`, `executor.go:329-344` | The 60/20/20 score in the README is really 0.6 x relevance | Medium |
| B23 | Collections use Chroma's default L2 space but scores assume cosine | `rag_service.go:139,244` | 0.7 relevance and 0.95 dedupe thresholds are meaningless | Medium |
| B24 | Ingest skips a chunk if it resembles another file's chunk | `rag_service.go:58-63` | Deleting the other file deletes this file's knowledge | Medium |
| B25 | `GetUserByID` and `GetConversationByID` return `nil, nil` when missing; callers dereference | `auth_service.go:121`, `chat_service.go:209` | Panics on deleted users and unknown ids | Medium |
| B26 | Files go to MinIO before the request is validated; multipart temp files never removed; filename unsanitized | `chat_handler.go:141-166` | Orphan objects and disk leak | Medium |
| B27 | One global "active model" shared by all requests | `model_manager.go` | Concurrent users unload each other's model mid-answer | Medium |
| B28 | Grounded answer calls `Chat` (body never closed) and then `Generate` | `orchestrator.go:171-189` | Two LLM calls per answer and a leaked connection | Medium |
| B29 | Password change skips the 8-character rule; >72-byte passwords return 500 | `auth_service.go:132` | Weak passwords; confusing errors | Low |
| B30 | Emails not lower-cased; register errors return 500 and say "user already exists" | `auth_service.go:39` | Duplicate accounts; account enumeration | Low |
| B31 | Date layout keeps the comma the input strips | `search_service.go:539` | Freshness is always 0.7 | Low |
| B32 | Summary sent with role `assistant`; summarizer re-reads already-summarized messages; its error is ignored | `chat_service.go:539,552` | Summaries drift and can silently fail | Low |
| B33 | Byte slicing `[:4000]` and `[:10000]` cuts UTF-8 mid-rune | `chat_service.go:440`, `search_service.go:511` | Garbled text in non-English files | Low |

Also dead or stale: `Validator` is never called, `CountTokens` and `js-tiktoken` are unused, `.env.example` documents Postgres and `LLM_BACKEND` that don't exist, and the README points to a `cmd/` directory that isn't there.

## Gaps vs Agentic AI: Zero to Hero

6 of 13 areas are missing outright; none is fully in place. The biggest holes are the ones your doc calls production table stakes: evals and tracing, guardrails, durable execution and tests.

| Zero to Hero section | What production needs | ConverseAI today | Status |
| --- | --- | --- | --- |
| 10. Evaluation and tracing | A golden set graded on faithfulness, context recall and tool-call accuracy; one trace per turn (OpenTelemetry to Langfuse or Phoenix) | No eval set; logging is `fmt.Printf`; the Mongo `events` collection is the only trace | Missing |
| 10. Guardrails and security (OWASP LLM Top 10) | Untrusted web and file text fenced off from instructions; output checks; rate limits; red-team suite (promptfoo or garak) | Scraped pages and uploads are pasted straight into planner and chat prompts (indirect prompt injection); SSRF (B18); no rate limits on login or completions | Missing |
| 11. Production: durable execution, reliability, rate limits | Runs survive restarts; retries with backoff and idempotency; queue between API and GPU; health checks | Each run lives inside one 300 s HTTP request; no retries; in-memory event broker pins you to one instance; no `/healthz` | Missing |
| 15. Testing | Unit tests for tools, mocked-model tests for the loop, recorded fixtures, CI gating merges | 0 test files, no CI, no linters; `main` merged in a non-compiling state | Missing |
| 16. Non-determinism | Pin temperature per call type (planner near 0), record model version and params per turn | `temperature`, `top_k`, `top_p` from `system_models.json` are never sent to Ollama | Missing |
| 6 and 8. Tool schemas and tool design | JSON Schema per tool, native tool calling, arguments validated before execution | Tools described in prose inside the planner prompt; arguments never reach the tool (B3); `Validator` exists but is never called | Missing |
| 3. Structured outputs | Schema-validated JSON with a repair or retry path | `format: json` plus a bracket-trimming `sCleanJSON`; one parse failure aborts the run | Partial |
| 7. The agent loop | Clear stop conditions, loop detection, a router you can evaluate | 5-step cap exists; agent vs chat is chosen by keyword match (`then`, `first`, `next`), which misroutes ordinary sentences | Partial |
| 8. Context engineering, memory, RAG | Only what this step needs in context; hybrid retrieval plus reranking; ingestion once, idempotent | 85% summarization trigger is a good start, but full files are stored in history (B13), re-ingested each turn (B12), fixed 1,000-char chunks, vector-only search | Partial |
| 13. Prompt engineering | Prompts versioned as files, iterated against evals | Prompts are inline Go strings; the planner prompt promises a Wikipedia tool that the planner can't select | Partial |
| 14. Model selection | Routing you can measure; pinned model versions | README promises OCR, vision, coding and translation routing; code sends OCR to the default model and never reads `DEFAULT_OCR/VISION/CODING/TRANSLATION_MODEL`; all images use `:latest` | Partial |
| 17. Data privacy | Everything sensitive encrypted, keys managed and rotatable, retention and delete-my-data | Message fields use AES-256-GCM, but events are plaintext (B14), the key defaults to a public string (B6), no rotation, no account deletion | Partial |
| 18. Human in the loop | Feedback capture on answers; approval gates for any write-capable tool | Current tools are read-only, so no gate is needed yet; no thumbs-up/down or correction capture to feed evals | Missing |

Outside the doc's scope but still blocking: no TLS termination, containers run as root, no backups for Mongo, MinIO or Chroma, and errors are returned to clients with raw `err.Error()` text.

## Gap plan

Six phases take ConverseAI to production in about 10 weeks for one engineer; Phases 0–2 (3.5 weeks) are pure bug-fixing and must finish before any public user touches it.

| Phase | Focus | Dates (2026) |
| --- | --- | --- |
| 0 | Make it build | Oct 5 to Oct 7 |
| 1 | Security holes | Oct 8 to Oct 16 |
| 2 | Correctness and reliability | Oct 19 to Oct 30 |
| 3 | Agent quality | Nov 2 to Nov 20 |
| 4 | Evals, tracing and tests (overlaps Phase 3) | Nov 9 to Nov 27 |
| 5 | Operate it | Nov 30 to Dec 11 |
| — | Production launch | Dec 14 |

Dates are estimates from the size of each phase's task list, not commitments; Phase 4 starts once Phase 3's tool schemas exist so evals grade the new loop, not the old one.

## Task checklist

Each phase ends at a gate; don't start the next phase's launch work until the gate passes. Bug IDs refer to the Bugs found table.

### Phase 0: Make it build

Gate: `go build ./...`, `go vet ./...` and `npm run build` pass in CI on every PR.

- [ ] Break the import cycle (B1): move `RagService` and `SearchService` interfaces into a package both sides import, or inject them into the executor as narrow interfaces defined in `orchestrator`
- [ ] Finish or revert the half-done tool-loop refactor (B2): delete the orphaned `Plan()` body, close `Execute`, remove the duplicate `emitEvent`, add missing imports and interface methods
- [ ] Pass `step` to `ExecuteStep` instead of `Execute(tool, reason, images)` (B3)
- [ ] Add a GitHub Actions workflow: Go build, vet, `staticcheck`, client lint and build
- [ ] One smoke test that registers, logs in, creates a conversation and streams a reply against a stub Ollama

### Phase 1: Close the security holes

Gate: a second test account cannot read, write or subscribe to the first account's data; the app refuses to start with default or empty secrets.

- [ ] Ownership check in `StreamCompletion` and `StreamEvents` (B4, B5); put it in one helper so no handler can skip it
- [ ] Fail fast on empty or default `JWT_SECRET` and `DB_ENCRYPTION_KEY`; treat empty env values as unset; pass both through compose and `deploy.sh` (B6, B7)
- [ ] Cookies `Secure` + `SameSite=Lax`; require `application/json` on JSON endpoints (B17)
- [ ] SSRF guard on page fetches: http(s) only, block private and link-local IPs after DNS resolution, cap redirects and body size (B18)
- [ ] Stop publishing Mongo, MinIO and Chroma ports; enable Mongo auth and non-default MinIO credentials (B20)
- [ ] Encrypt or drop prompt text in `events` (B14)
- [ ] Nil checks after every `GetBy*` (B25); password rules on change; lower-case emails; 409 for duplicates (B29, B30)
- [ ] Rate limits on login, register and completions
- [ ] Return generic error messages to clients; log the detail server-side

### Phase 2: Correctness and reliability

Gate: a 20-turn conversation with a PDF, an image and a web search completes with correct formatting, no duplicate vectors and no goroutine leaks under `-race`.

- [ ] Make the completion goroutine stop writing once the client leaves; cancel its context (B8)
- [ ] Separate dial/header timeouts from stream reading on the Ollama client; surface scanner errors (B9)
- [ ] Send `keep_alive: 0` explicitly to unload (B10); cap `num_ctx` per model to what the GPU holds (B11)
- [ ] Store the user's own text plus attachment ids; build file context at request time; ingest each file once, keyed by content hash (B12, B13, B24)
- [ ] Proxy file downloads through the API or sign URLs with a public MinIO endpoint (B15)
- [ ] Send SSE payloads as JSON (`data: {"delta":"..."}`) and parse events properly on the client (B16)
- [ ] Move Chroma calls to the API version your Chroma image serves; check status codes; create collections with cosine space; pin the image (B19, B23)
- [ ] Route the Files tab GET to `ListConversationFiles` (B21)
- [ ] Carry authority and freshness through ranking; apply conflict flags to the ranked items; fix the date layout (B22, B31)
- [ ] Validate requests before uploading; remove multipart temp files; sanitize filenames (B26)
- [ ] Per-request model handling instead of one global active model, or a GPU job queue (B27)
- [ ] One LLM call for the grounded answer (B28); fix summary role and inputs (B32); rune-safe truncation (B33)
- [ ] Add `/healthz` and `/readyz` checking Mongo, MinIO, Chroma and Ollama

### Phase 3: Agent quality

Gate: the planner's tool calls validate against schema on 100% of the golden set, and routing accuracy beats the keyword router.

- [ ] Define each tool with a JSON Schema and use Ollama's native `tools` field; validate arguments before execution (wire up `Validator`)
- [ ] Replace keyword `needsOrchestration` with a small classifier call or let the agent decide with a `respond_directly` tool
- [ ] Retry-with-error-feedback when structured output fails to parse
- [ ] Fence untrusted content (web pages, file text) in tagged blocks and tell the model it is data, not instructions
- [ ] Move prompts into versioned files; log prompt version per run
- [ ] Send per-model `temperature`, `top_k`, `top_p`; planner at temperature 0
- [ ] Implement the routing the README promises (OCR, vision, coding, translation) or remove it from the README
- [ ] Hybrid retrieval (BM25 + vector) and a reranker; token-aware chunking

### Phase 4: Evals, tracing and tests

Gate: eval suite runs in CI and blocks merges that drop faithfulness or tool-call accuracy below the agreed baseline.

- [ ] Golden set: 50 questions across chat, file Q&A, web search and multi-step, each with required facts and sources
- [ ] Metrics: faithfulness, context recall, answer relevance, tool-call accuracy, goal accuracy (Ragas or DeepEval)
- [ ] OpenTelemetry traces per turn exported to self-hosted Langfuse or Phoenix; replace `fmt.Printf` with structured `slog`
- [ ] Prometheus metrics: latency per stage, tokens, tool errors, queue depth
- [ ] Unit tests for tools, storage, crypto and auth; mocked-model tests for the agent loop; recorded Ollama fixtures
- [ ] Red-team suite (promptfoo or garak) for prompt injection via uploaded files and web pages
- [ ] Thumbs up/down and correction capture in the UI, fed into the golden set

### Phase 5: Operate it

Gate: restore-from-backup drill succeeds; a restart mid-answer loses no saved work.

- [ ] TLS in front (Caddy or Traefik); non-root container user; pinned image tags; compose health checks
- [ ] Nightly backups of Mongo, MinIO and Chroma volumes with a tested restore
- [ ] Queue agent runs (Redis Streams or NATS) with persisted step state so runs resume after restart
- [ ] Move the event broker to Redis pub/sub so more than one API instance can run
- [ ] Key rotation for `DB_ENCRYPTION_KEY`; account and data deletion endpoint covering Mongo, MinIO and Chroma
- [ ] Indexes on `conversations.user_id` and `events.conversation_id`; stop returning full message bodies in the conversation list
- [ ] Rewrite `.env.example` and README to match the real stack

# ConverseAI

A self-hosted, multimodal AI chat app: a Go backend and a React client in
front of local models served by [Ollama](https://ollama.com). It answers from
your uploaded files, can search the web, cites its sources, and keeps every
conversation encrypted at rest.

## Features

- **Tool-calling agent.** The chat model uses Ollama's native tool calling.
  It decides per message whether to answer directly or call tools:
  - `web_search`: DuckDuckGo with a Wikipedia fallback. Reads the top pages and ranks passages by relevance (60%), domain authority (20%) and freshness (20%). Flags sources that contradict each other.
  - `search_documents`: hybrid retrieval over the files attached to the conversation.
  - `extract_text_from_image`: OCR with a dedicated model.
  - `translate`: translation with a dedicated model.

  Tool arguments are validated against a JSON Schema. A malformed call is sent back to the model with the error, so it can fix it. Repeated calls are detected, and after 5 rounds the model must answer.
- **Grounded answers.** Retrieved passages are numbered, and the model cites them as `[n]`. The saved answer ends with a list of the sources it actually cited.
- **Files.**
  - **Supported types:** images, PDFs and text or code files.
  - **Small text files:** go into the prompt whole.
  - **Indexing:** every text file is indexed once in Chroma: about 400-token chunks, cosine similarity, then BM25 re-ranking fused with reciprocal rank fusion.
  - **Storage:** identical uploads are stored once.
  - **Deletion:** removing a file from one conversation purges it only when no other conversation uses it.
- **Model routing.**
  - **Images:** if they arrive for a model without vision, the turn moves to `DEFAULT_VISION_MODEL`.
  - **OCR and translation:** each tool has its own model.
  - **GPU:** with `SINGLE_MODEL_MODE`, requests take turns on the GPU, so one request never unloads a model another is still using.
- **Durable answers.**
  - **Background runs:** each answer runs in the background and is saved after every tool round.
  - **Disconnects and reloads:** if the browser closes the answer still finishes, and after a reload the page re-attaches to it.
  - **Restarts:** if the server restarts mid-answer, the next instance resumes it from the last saved round.
- **Long conversations.** When the history would fill the context window (capped by `MAX_NUM_CTX`), older turns are summarized.
- **Security.**
  - Every conversation, run, event and file is checked against its owner.
  - **Encryption at rest:** messages, titles, summaries, event logs, run state and feedback use AES-256-GCM, with key rotation.
  - **Untrusted text:** web pages, files and tool output are fenced as data, so instructions inside them aren't followed.
  - **Web fetching:** an SSRF guard refuses private, loopback, link-local and Tailscale addresses.
  - **Requests:** rate limits; `SameSite` cookies plus an Origin check against CSRF; generic error messages.
- **Operations.**
  - Structured JSON logs, `/healthz`, `/readyz` and Prometheus metrics on `/metrics`.
  - Optional OpenTelemetry traces.
  - Backup and restore scripts with a tested restore drill.
  - A golden-set eval runner.

## Stack

| Part | Technology |
| --- | --- |
| Backend | Go 1.25, `net/http`, `log/slog` |
| Client | React 19, Vite, TypeScript |
| Models | Ollama (chat, vision, OCR, translation, embeddings) |
| Database | MongoDB 8 |
| Vectors | ChromaDB 1.x (v2 API) |
| Files | MinIO (S3-compatible) |
| Optional | Redis (multi-instance streaming), Caddy (TLS), OTLP collector (traces) |

## Quick start (Docker)

1. Install Ollama on the host and pull the models you want:

   ```sh
   ollama pull gemma4:latest
   ollama pull nomic-embed-text-v2-moe:latest
   ollama pull qwen3-vl:8b deepseek-ocr:3b translategemma:12b   # optional
   ```
2. Configure secrets. The app refuses to start with empty or placeholder secrets.

   ```sh
   cp .env.example .env
   # JWT_SECRET:        openssl rand -hex 32
   # DB_ENCRYPTION_KEY: openssl rand -hex 16   (exactly 32 characters)
   # MONGO_PASSWORD, MINIO_ROOT_PASSWORD: openssl rand -hex 16
   ```
3. Start:

   ```sh
   ./deploy.sh            # or: docker compose up -d --build --wait
   ./deploy.sh --tls      # also Caddy on :80/:443, see deploy/Caddyfile
   ```

   Open `http://<host>:8080` and create an account.

**HTTPS and cookies.** Session cookies are `Secure` by default, so browsers only keep them over HTTPS (or on `localhost`). Put the app behind HTTPS (the bundled Caddy profile, or `tailscale serve`) and set `TRUST_PROXY=true`. Use `COOKIE_SECURE=false` only for plain HTTP on a trusted LAN.

**Network layout.** Only the app (and Caddy) publish ports. MongoDB, MinIO and Chroma sit on an internal Docker network with no host ports and no internet access. The app container runs as a non-root user on a read-only filesystem.

### Upgrading an existing deployment

- **MongoDB:** it now requires authentication. `MONGO_USER`/`MONGO_PASSWORD` only take effect on an empty data volume. For an existing volume, either create that user in it (`db.createUser` with the `root` role in `admin`) or start with a fresh volume.
- **Chroma:** it moved to the 1.x image (`/data`). Older vector data isn't read, but files are re-indexed automatically the next time their conversation is used.
- **Old System Logs:** events from older versions stored prompts in plaintext; those payloads are removed on first start.
- **MinIO image:** `minio/minio` is no longer published on Docker Hub. Compose uses a community build (`pgsty/minio`); override it with `MINIO_IMAGE`.

## Configuration

Everything is set through environment variables; see [`.env.example`](.env.example). The ones that matter most:

| Variable | Default | Purpose |
| --- | --- | --- |
| `MAX_NUM_CTX` | `8192` | Upper bound on the context window sent to Ollama (the KV cache must fit in VRAM) |
| `SINGLE_MODEL_MODE` | `true` | Keep one chat model loaded at a time |
| `MAX_CONCURRENT_RUNS` | `2` | Answers generated at the same time; the rest queue |
| `RUN_TIMEOUT` | `15m` | Upper bound for one answer |
| `MAX_UPLOAD_MB` | `25` | Total upload size per message |
| `REDIS_URL` | empty | Enables the Redis event broker, so several app instances can share live streams |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty | Enables trace export (e.g. self-hosted Langfuse or Phoenix) |

The model catalog (context windows, sampling defaults, capabilities) lives in [`internal/repository/system_models.json`](internal/repository/system_models.json). Only models listed there can be selected. Each model's `temperature`, `top_k`, `top_p`, `repeat_penalty` and `stop` are sent with every request. Every run records its prompt version, the models used and the model parameters.

## Operations

| Task | How |
| --- | --- |
| Health | `GET /healthz` (process), `GET /readyz` (MongoDB, MinIO, Chroma, broker; Ollama reported but optional) |
| Metrics | `GET /metrics`: request latency, model-call latency, tokens, tool calls and errors, run stages, queue depths, rate-limit hits |
| Backup | `scripts/backup.sh`: app database, uploaded files and vectors, with checksums; keeps 14. Nightly cron: `0 3 * * * cd /path/to/ConverseAI && scripts/backup.sh >> backups/backup.log 2>&1` |
| Restore | `scripts/restore.sh backups/<timestamp>`: verifies checksums, then replaces the app's data |
| Rotate the encryption key | Move the current key to `DB_ENCRYPTION_KEYS_OLD`, set a new `DB_ENCRYPTION_KEY`, restart, run `docker compose exec converseai converseai rotate-keys`, then remove the old key |
| Delete an account | Settings → Security → Delete account. Removes the user's conversations, events, runs, feedback, files and vectors |
| Collect bad answers | Users rate answers with 👍/👎 (with an optional correction). `docker compose exec converseai converseai export-feedback` prints the 👎 answers as JSONL candidates for the golden set |

## Development

```sh
make build-client          # React client into client/dist (embedded in the binary)
APP_ENV=development make dev-server
make dev-client            # Vite dev server
make test                  # unit tests (race detector)
make test-integration      # end-to-end tests against MongoDB, MinIO and Chroma in Docker
make lint
```

- **Integration tests:** they start the services in [`docker-compose.ci.yml`](docker-compose.ci.yml) and drive the real HTTP API with a fake model, covering ownership checks, encryption at rest, file Q&A, tool calls, cancel, resume after restart and account deletion.
- **CI:** [`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs build, vet, staticcheck, unit and integration tests, the client lint and build, and the Docker build.

### Evals

[`evals/golden.jsonl`](evals/golden.jsonl) has 50 cases: direct chat, file Q&A, follow-ups, web search, multi-step questions and prompt-injection red-team cases (fixtures in `evals/fixtures/`). `cmd/eval` runs them against a live instance and scores:

- tool-call accuracy
- answer correctness
- injection resistance
- citation rate
- faithfulness (optional, judged by an LLM with `-judge-model`)

It fails if any score drops below [`evals/baseline.json`](evals/baseline.json).

```sh
go run ./cmd/eval -base-url http://localhost:8080 -model gemma4:latest
```

These evals need real models, so they run on demand ([`.github/workflows/evals.yml`](.github/workflows/evals.yml), on a self-hosted runner that can reach Ollama). After the first real run, raise the baseline to the scores you get.

## Repository layout

```
main.go                  serve | rotate-keys | export-feedback
cmd/eval/                golden-set eval runner
internal/agent/          tool-calling loop, tools, versioned prompts
internal/app/            wiring and routes
internal/service/        auth, conversations, background runs
internal/rag/            Chroma v2 client, chunking, hybrid ranking
internal/search/         web search and SSRF-safe page fetching
internal/repository/     MongoDB repositories (encrypted fields)
internal/manager/        GPU model leases
internal/events/         live event broker (memory or Redis)
internal/handler/        HTTP handlers, SSE
internal/middleware/     auth, rate limits, security headers, logging
internal/e2e/            end-to-end tests
client/                  React app
scripts/                 backup and restore
deploy/                  Caddyfile
evals/                   golden set, fixtures, baseline
```

## Limitations

- **Speed:** depends on your hardware. CPU-only Ollama works but is slow for 8B+ models.
- **Model switching:** with `SINGLE_MODEL_MODE`, switching models (e.g. for OCR or vision) unloads the previous one, which adds load time.
- **Web search:** it scrapes DuckDuckGo's HTML page, which rate-limits automated use. Wikipedia is the fallback.
- **BM25:** it re-ranks the vector search's top candidates, not the whole corpus.

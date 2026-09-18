# AGENTS.md

Kabanbot is a Telegram group bot. It caches chat messages and summarizes them with an LLM: `/summary` sent as a reply summarizes everything from that message onward. It also pings everyone on `@all`.

**Status:** being rebuilt in Go in phases.
- Phase 1 is done: parity with the old Python bot, plus pluggable providers and Rich Markdown.
- Phase 2: a Telegram Mini App for managing providers and models and binding them to groups.
- Phase 3: chatting with the bot, and a per-chat personality editable via LLM tool calls (admins only).

The old Python bot is tagged `old`.

## Commands

- `make run` — run locally (reads env; see `.env.example`)
- `make test` — `go test -race ./...`. Single test: `go test ./internal/storage/sqlite -run TestMigratesLegacyPythonSchema`
- `make lint` — golangci-lint v2 plus `go fix -diff`. `make fmt` formats with gofumpt and goimports.
- `make generate` — regenerate sqlc code after editing `internal/storage/sqlite/{migrations,queries}`. The generated code is committed; CI checks it is up to date.
- `docker compose up -d --build` — production-like run with `./data` mounted.

## Architecture

Ports & adapters. Use cases never import Telegram, SQL or provider SDKs.

- `cmd/kabanbot` only wires things together and handles shutdown.
- `internal/app/*` holds use cases (`ingest`, `summary`, `mention`). Each declares the small interfaces it consumes.
- `internal/llm` is the provider-agnostic port (`Client`, `Request`, `Target`), with adapters `llm/openai` (also used for OpenRouter) and `llm/gemini`.
- `llm/registry` resolves which model a chat uses. It returns the chat's bound model from the DB, or the env default (`LLM_*`).
- `internal/storage/sqlite`: SQLite (`ncruces/go-sqlite3`, no CGO), goose migrations embedded, sqlc queries in `sqlcgen`.
  - Migration `00001` is the Python schema, so old `data/messages.db` files upgrade in place.
  - Provider API keys are stored AES-GCM encrypted (`internal/secrets`, `MASTER_KEY`) and must never be returned or logged. `domain.Secret` redacts itself.
- `internal/telegram` is the Bot API adapter (`mymmrac/telego`, Bot API 10.3).
  - Messages are ingested synchronously in update order; slow handlers run in bounded goroutines.
  - Replies use **Rich Messages** (`sendRichMessage` with GFM-like `markdown`, up to 32768 chars), with a fallback to plain text.
  - Service notices go out as **ephemeral** messages that only the caller sees.
- `prompts/` holds the embedded system prompts. The summary prompt describes the Rich Markdown dialect to the model.

## Conventions

- Go 1.27 idioms: `encoding/json/v2`, `new(expr)`, `wg.Go`, `t.Context()`, `slog`.
- Messages shown to users are in Russian.
- The bot needs privacy mode disabled to see every group message.

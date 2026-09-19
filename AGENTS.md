# AGENTS.md

Kabanbot is a Telegram group bot. It caches chat messages and summarizes them with an LLM: `/summary` sent as a reply summarizes everything from that message onward. It also pings everyone on `@all`. Inline-button menus let users connect their own LLM providers and models (in private chat) and configure each group.

**Status:** being rebuilt in Go in phases; see **`docs/ROADMAP.md`** for the detailed plan.
- Phases 1–3 are done. Phase 3 replaced the Mini App (phase 2) with inline-button menus, so the bot needs no public HTTPS site.
- **Next is phase 4:** chatting with the bot, and a per-chat personality editable via LLM tool calls (admins only).

The old Python bot is tagged `old`.

## Commands

- `make run` — run locally (reads env; see `.env.example`)
- `make test` — `go test -race ./...`. Single test: `go test ./internal/app/settings -run TestChatAccessAndBinding`
- `make lint` — golangci-lint v2 plus `go fix -diff`. `make fmt` formats with gofumpt and goimports.
- `make generate` — regenerate sqlc code after editing `internal/storage/sqlite/{migrations,queries}`. The generated code is committed; CI checks it is up to date.
- `docker compose up -d --build` — production-like run with `./data` mounted.

## Architecture

Ports & adapters. Use cases never import Telegram, SQL or provider SDKs.

- `cmd/kabanbot` only wires things together and handles shutdown. An HTTP server on `HTTP_ADDR` serves only `/healthz`.
- `internal/app/*` holds use cases (`ingest`, `summary`, `mention`, `settings`). Each declares the small interfaces it consumes.
- **`app/settings` holds every settings authorization rule.** The Telegram menus call it and never check rights themselves.
  - Only the owner can see or change a provider or model; other users get `ErrNotFound`.
  - API keys are write-only: never returned, only `key_hint`.
  - Changing `base_url` requires re-entering the key.
  - Only `OWNER_ID` may mark a provider `shared`; its models can then be picked in any group.
  - Chat settings need chat-admin rights (`getChatMember`, cached for 5 min). Binding a model requires it to be the user's own or shared; keeping a model someone else bound is allowed.
  - A model's owner can unbind it from any chat.
  - `Validate*` functions check single fields so the menus can reject input step by step; the service checks them again.
- `internal/llm` is the provider-agnostic port (`Client`, `Request`, `Target`), with adapters `llm/openai` (also used for OpenRouter) and `llm/gemini`.
  - Adapters turn HTTP and network failures into `*llm.Error` (status, provider message, `Retry-After` or Gemini's `RetryInfo`).
  - `llm.Retrying` retries 429, 5xx, timeouts and dropped connections with growing delays; SDK retries are off. `llm.WithRetryObserver` lets the caller show progress.
- `llm/registry` resolves which model a chat uses: its bound model. There is no default model; an unbound chat gets `domain.ErrNoModel`. Clients of users' providers go through `internal/netguard`, which blocks private and loopback addresses (SSRF). Only `OWNER_ID`'s providers may reach local servers.
- `internal/storage/sqlite`: SQLite (`ncruces/go-sqlite3`, no CGO), goose migrations embedded, sqlc queries in `sqlcgen`.
  - Migration `00001` is the Python schema, so old `data/messages.db` files upgrade in place.
  - Provider keys are AES-GCM encrypted by the store (`internal/secrets`, `MASTER_KEY`, which is required). `domain.Secret` redacts itself in logs.
- `internal/telegram` is the Bot API adapter (`mymmrac/telego`, Bot API 10.3).
  - Messages are ingested synchronously in update order; slow handlers run in bounded goroutines.
  - `my_chat_member` updates and group messages keep the `chats` table current.
  - Replies use **Rich Messages** (`sendRichMessage` with GFM-like `markdown`), with a fallback to plain text. Service notices are **ephemeral**.
  - `/summary` replies at once with a public status message, updates it on retries, and edits it into the summary or a readable error (`describeLLMError`).
  - **Settings menus** (`menu.go` renders screens as data, `menu_bot.go` does the Bot API calls). One message is one screen; buttons edit it in place.
    - Callback data is `op[:id[:id2]][:word]`, at most 64 bytes, parsed only by `parseRoute` in `route.go`. It is untrusted: every press re-checks rights through `app/settings`, and a group menu only manages its own group.
    - `/settings` in a group sends an **ephemeral** menu (switches, summary model) that only the calling admin sees. Providers, keys and models live only in private chat (`errPrivateOnly`), reached by `/start`, `/settings` or `t.me/<bot>?start=models`.
    - Text input is a per-user dialog in memory (`dialog.go`, 10-minute TTL, `/cancel` or any command ends it). The bot asks with `ForceReply`, and API-key messages are deleted as soon as they arrive.
- `prompts/` holds the embedded system prompts. The summary prompt describes the Rich Markdown dialect to the model.

## Conventions

- Go 1.27 idioms: `encoding/json/v2`, `new(expr)`, `errors.AsType`, `wg.Go`, `t.Context()`, `slog`.
- Messages shown to users are in Russian.
- The bot needs privacy mode disabled to see every group message.

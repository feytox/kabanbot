# AGENTS.md

Kabanbot is a Telegram group bot. It caches chat messages and summarizes them with an LLM: `/summary` sent as a reply summarizes everything from that message onward. It also pings everyone on `@all`. A Telegram Mini App lets users connect their own LLM providers and models and configure each group.

**Status:** being rebuilt in Go in phases.
- Phase 1 is done: parity with the old Python bot, plus pluggable providers and Rich Markdown.
- Phase 2 is done: the Mini App for providers, models and per-group settings.
- Phase 3: chatting with the bot, and a per-chat personality editable via LLM tool calls (admins only).

The old Python bot is tagged `old`.

## Commands

- `make run` — run locally (reads env; see `.env.example`)
- `make test` — `go test -race ./...`. Single test: `go test ./internal/app/settings -run TestChatAccessAndBinding`
- `make lint` — golangci-lint v2 plus `go fix -diff`. `make fmt` formats with gofumpt and goimports.
- `make generate` — regenerate sqlc code after editing `internal/storage/sqlite/{migrations,queries}`. The generated code is committed; CI checks it is up to date.
- `make web` — build the Mini App (`web/miniapp`: React 19, Vite, `@tma.js/sdk-react` v3) into `web/miniapp/dist`. The Go binary embeds that directory. Without a build it serves "not built". `npm run dev` in `web/miniapp` proxies `/api` to `:8080`.
- `docker compose up -d --build` — production-like run with `./data` mounted. The image builds the Mini App too.

## Architecture

Ports & adapters. Use cases never import Telegram, SQL or provider SDKs.

- `cmd/kabanbot` only wires things together and handles shutdown. One HTTP server on `HTTP_ADDR` serves `/healthz`, the Mini App and its API.
- `internal/app/*` holds use cases (`ingest`, `summary`, `mention`, `settings`). Each declares the small interfaces it consumes.
- **`app/settings` holds every Mini App authorization rule.**
  - Only the owner can see or change a provider or model; other users get `ErrNotFound`.
  - API keys are write-only: never returned, only `key_hint`.
  - Changing `base_url` requires re-entering the key.
  - Only `OWNER_ID` may mark a provider `shared`.
  - Chat settings need chat-admin rights (`getChatMember`, cached for 5 min). Binding a model requires it to be the user's own or shared; keeping a model someone else bound is allowed.
  - A model's owner can unbind it from any chat.
- `internal/webapi` is the JSON API plus SPA serving. Requests authenticate with `Authorization: tma <initData>`, checked by HMAC in `initdata.go` and valid for 24 h.
- `internal/llm` is the provider-agnostic port (`Client`, `Request`, `Target`), with adapters `llm/openai` (also used for OpenRouter) and `llm/gemini`.
- `llm/registry` resolves which model a chat uses: its bound model, or the env default (`LLM_*`). Clients of users' providers go through `internal/netguard`, which blocks private and loopback addresses (SSRF). Only `OWNER_ID`'s providers and the env default may reach local servers.
- `internal/storage/sqlite`: SQLite (`ncruces/go-sqlite3`, no CGO), goose migrations embedded, sqlc queries in `sqlcgen`.
  - Migration `00001` is the Python schema, so old `data/messages.db` files upgrade in place.
  - Provider keys are AES-GCM encrypted by the store (`internal/secrets`, `MASTER_KEY`). `domain.Secret` redacts itself in logs.
- `internal/telegram` is the Bot API adapter (`mymmrac/telego`, Bot API 10.3).
  - Messages are ingested synchronously in update order; slow handlers run in bounded goroutines.
  - `my_chat_member` updates and group messages keep the `chats` table current.
  - Replies use **Rich Messages** (`sendRichMessage` with GFM-like `markdown`), with a fallback to plain text. Service notices are **ephemeral**.
  - `/settings` in a group sends an ephemeral link `t.me/<bot>?startapp=chat_<id>`, which needs the Main Mini App set in BotFather. The Mini App opens that chat directly.
- `prompts/` holds the embedded system prompts. The summary prompt describes the Rich Markdown dialect to the model.

## Conventions

- Go 1.27 idioms: `encoding/json/v2`, `new(expr)`, `errors.AsType`, `wg.Go`, `t.Context()`, `slog`.
- Messages shown to users (bot and Mini App) are in Russian.
- The bot needs privacy mode disabled to see every group message.
- Mini App styling uses Telegram theme CSS variables (`--tg-theme-*`) directly. `@telegram-apps/telegram-ui` is not used because it doesn't support React 19.

# AGENTS.md

Kabanbot is a Telegram group bot that caches chat messages and summarizes them with an LLM (`/summary` as a reply summarizes everything from that message onward). It also pings everyone on `@all`.

**Status:** this code is being migrated from Python to Go and cleaned up along the way. Don't treat the current Python layout as the target architecture. The Python baseline is tagged `old`, and an earlier rough Go attempt lives on the `experiment/go` branch.

## Run

- Local: `uv sync && uv run python -m kabanbot` (Python 3.13+)
- Docker: `docker compose up -d --build` (mounts `./data` for the SQLite DB)
- Config comes from `.env` (see `kabanbot/config.py`): `BOT_TOKEN`, `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL`, `DB_PATH`, `CACHE_SIZE`, `ALLOWED_GROUPS`.

There are no tests or linters yet.

## Architecture (Python, current)

- `__main__.py` wires everything together: aiogram `Dispatcher`, the middlewares, and services injected via `dp["cache"]` / `dp["llm"]`.
- Middlewares on the group router run in order: `WhitelistMiddleware` (drops chats not in `ALLOWED_GROUPS`; an empty list allows all) → `CacheMiddleware` (stores every non-command message, with placeholders for media).
- `services/cache.py` holds the SQLite (aiosqlite) message store, capped at `CACHE_SIZE` per chat. `services/llm.py` calls litellm using the system prompt from `prompts/summary.md`.
- `handlers/groups.py` has `/summary` and `@all`. The LLM output is converted to Telegram markdown via `telegramify-markdown`.

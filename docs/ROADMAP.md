# Roadmap

Phase 1 is done: parity with the Python bot, three LLM providers, Rich Markdown.

Phase 2 is done: a Mini App for providers, models and group settings. We dropped it because a Mini App needs a separately hosted public HTTPS site, and we don't want to run one.

Phase 3 is implemented: inline buttons in Telegram itself replace the Mini App. What remains is the manual check at the end of its section.

## Phase 3 — settings through inline buttons instead of the Mini App

**Goal:** everything the Mini App could do (providers, models, keys, model test, per-group settings, unbinding) becomes screens made of messages with inline keyboards. The bot then needs no public HTTP server beyond `/healthz`.

### Keep as is

- `internal/app/settings` with all authorization rules and its tests. The new UI calls the same methods: `Providers`, `CreateProvider`, `UpdateProvider`, `TestModel`, `UpdateChat`, and so on. **Do not copy the authorization checks into the Telegram adapter.**
- `internal/storage/sqlite` (models, chats, users, migration `00004`), `internal/secrets`, `internal/netguard`, `llm/registry`.
- Chat tracking (`my_chat_member`, `TouchChat`), checking chat settings before `/summary` and `@all`, and `Client.IsAdmin` with its cache.

### Remove

- `internal/webapi` (API, `initData`), `web/` (the Mini App and `embed.go`), and in `cmd/kabanbot/main.go` the wiring of `webapi` and `web.MiniApp()`. The HTTP server keeps only `/healthz`.
- `WEBAPP_URL`: from `config`, `.env.example`, `telegram.Deps`, `SetChatMenuButton` and the `web_app` button.
- The Node stage in the `Dockerfile`, the `miniapp` job in CI, the `web` target in the `Makefile`, and the `web/miniapp` lines in `.gitignore` and `.dockerignore`.
- The `ports` block in `docker-compose.yml`, which was only there for the Mini App.
- The Mini App parts of `AGENTS.md`.

### Build (in `internal/telegram`, a new `ui` subpackage or `menu_*.go` files)

**Screens.** One message is one screen. Navigation edits it in place (`editMessageText` plus `reply_markup`) instead of sending new messages.

**Callback data.** At most 64 bytes, in a compact scheme such as `g:<chatID>`, `gt:<chatID>:sum` (toggle), `gm:<chatID>:<modelID>`, `p:<id>`, `pd:<id>` (delete), `m:<id>`, `mt:<id>` (test), `mu:<id>:<chatID>` (unbind), `back:<screen>`. Parse it in one place and fail safely on garbage.

**Authorization.** Every callback re-checks rights through `app/settings`. A callback must be treated as untrusted input: someone else may press the button, and data can be forged. Answer `answerCallbackQuery` with an error toast.

**Where the screens live:**
- **Group settings** open right inside the group. `/settings` sends an **ephemeral** message (Bot API 10.2+) with an inline keyboard: the on/off switches and the model choice. Navigation uses `editEphemeralMessageText`; for callbacks, see `replace_callback_query_message` in `EphemeralMessageParameters`. Only the admin who called it sees the menu, so the group chat stays clean.
- **Providers, keys and models** live only in private chat with the bot. **Keys must never be typed in a group.** The group menu gets a button «Мои модели → в личку»: a URL button to `t.me/<bot>?start=models`.
- `/start` and `/settings` in private chat open the main menu with «Группы» (groups where the user is an admin, each opening the same settings screen) and «Мои модели».

**Text input** (provider name, base URL, key, model ID, display name, temperature, max_tokens):
- Keep a per-user dialog state in memory: `map[userID]pending` with a 10-minute TTL. Losing it on restart is acceptable.
- The flow: the bot asks with `ForceReply`, the next private message from the user is taken as the answer, and `/cancel` aborts.
- **API key:** delete the user's message right after receiving it (`deleteMessage`), so the key doesn't stay in the chat history. Then reply «Ключ сохранён ••••1234».
- Validation errors from `settings.ValidationError` are shown as they are, and the bot waits for input again.

**Modern Bot API features to use:**
- `style` on buttons (Bot API 9.4): `danger` for delete, `success` for save/enable.
- `DisabledButton` (10.3) for inactive items, e.g. feature switches while the bot is turned off.
- Confirm deletion with a second screen offering «Да, удалить» / «Отмена».

### Tests

- Callback data encoding and decoding: round-trip, and garbage must not panic.
- The dialog state machine (steps, TTL, `/cancel`), with a fake sender and `testing/synctest` for the TTL.
- Authorization is already covered by the `app/settings` tests; add only a test that a callback from a non-admin is rejected.

### Check by hand

In a test group:
- `/settings` shows the ephemeral menu only to the admin, and toggles and model choice are saved;
- a non-admin who opens the same menu (e.g. through a forwarded button) gets refused.

In private chat:
- the flow provider → key (the message is deleted) → model → «Проверить» → bind to a group works;
- `/summary` then uses the bound model.

## Phase 4 — chatting with the bot and personality (formerly phase 3)

**Triggers.** The bot answers:
- a mention of `@bot`;
- a reply to the bot's message;
- `/ask`;
- any message in private chat.

**Context.** The last N messages of the chat plus a system prompt with the chat's personality. The bot's own replies must go into `messages` (`is_bot`) so it can see its side of the conversation.

**Chat model.** The `chats.chat_model_id` column already exists. Add model selection for chatting to the group menu from phase 3, in the same way as `summary_model_id`, with the same rule: only the user's own models. Also add `ChatTarget` to `llm/registry`.

**Streaming:**
- In private chat use `sendRichMessageDraft`: first a thinking block (`RichBlockThinking`), with `can_stop`. A `stopped_message_generation` update cancels the generation `ctx`. Finish with `sendRichMessage`.
- In groups, throttle `editMessageText(rich_message)` to about once per 1.5 s.
- Add `Stream(ctx, Request) iter.Seq2[Chunk, error]` to the `llm.Client` port.

**Tool calling:**
- A generic `llm.Tool` / `ToolCall` in the port, supported in both adapters: openai-go and genai function calling.
- The loop in `app/chat` is capped at 5 iterations.

**Personality tools** (`get_personality`, `set_personality`, `reset_personality`, `set_summary_style`):
- They are passed to the model **only if the message author is a chat admin**.
- Rights are **re-checked** when a tool runs; don't trust the model.
- Every change goes into `personality_history(chat_id, text, changed_by, via[menu|tool], created_at)`.

**Personality UI.** In the group menu from phase 3, a «Личность» item:
- shows the current text;
- «Изменить» enters text through the dialog;
- «История» lists changes with a «Вернуть» button;
- «Сбросить».

**Limits and accounting:**
- Rate limiting per chat and per user, configured in `settings_json`. Test it with `synctest`.
- Record `llm_usage(chat_id, user_id, model_id, tokens_in, tokens_out, created_at)`.
- Show «Статистика» in the group menu.

**Check by hand:**
- a non-admin asks to change the personality: the tools are not offered, and nothing changes;
- an admin can change it, and the change shows up in «Личность → История»;
- in private chat the streaming draft appears, and «стоп» interrupts generation.

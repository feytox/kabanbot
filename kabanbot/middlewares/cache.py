from typing import Callable, Dict, Any, Awaitable
from aiogram import BaseMiddleware
from aiogram.types import Message, TelegramObject
from kabanbot.services.cache import MessageCache
from kabanbot.consts import BOT_COMMANDS


def _get_content_placeholder(message: Message) -> str | None:
    if message.photo:
        return "[Photo]"
    if message.video:
        return "[Video]"
    if message.voice:
        return "[Voice]"
    if message.audio:
        return "[Audio]"
    if message.document:
        return "[Document]"
    if message.sticker:
        emoji = message.sticker.emoji or ""
        return f"[Sticker] {emoji}".strip()
    if message.animation:
        return "[GIF]"
    if message.video_note:
        return "[Video Note]"
    if message.poll:
        return f"[Poll] {message.poll.question}"
    if message.location:
        return "[Location]"
    if message.contact:
        return "[Contact]"
    return None


class CacheMiddleware(BaseMiddleware):
    def __init__(self, cache: MessageCache):
        super().__init__()
        self.cache = cache
        self.bot_username = None

    async def __call__(
        self,
        handler: Callable[[TelegramObject, Dict[str, Any]], Awaitable[Any]],
        event: TelegramObject,
        data: Dict[str, Any],
    ) -> Any:
        if isinstance(event, Message):
            if self.bot_username is None:
                bot = data.get("bot")
                if bot:
                    me = await bot.get_me()
                    self.bot_username = me.username

            caption_or_text = event.text or event.caption or ""
            placeholder = _get_content_placeholder(event)

            if placeholder:
                text = f"{placeholder} {caption_or_text}".strip()
            else:
                text = caption_or_text

            should_save = True
            if text.startswith("/"):
                command_parts = text.split()[0].split("@")
                command = command_parts[0][1:]
                target_bot = command_parts[1] if len(command_parts) > 1 else None

                is_my_command = False
                if target_bot:
                    if (
                        self.bot_username
                        and target_bot.lower() == self.bot_username.lower()
                    ):
                        is_my_command = True
                else:
                    if command in [c.command for c in BOT_COMMANDS]:
                        is_my_command = True

                if is_my_command:
                    should_save = False

            if text and should_save:
                user_id = event.from_user.id if event.from_user else 0
                username = (
                    event.from_user.username or event.from_user.full_name or "Unknown"
                )

                reply_to_text = None
                reply_to_username = None

                if event.reply_to_message:
                    reply_msg = event.reply_to_message
                    details = await self.cache.get_message_details(
                        chat_id=event.chat.id,
                        message_id=reply_msg.message_id,
                    )
                    if details:
                        reply_to_text = details["text"]
                        reply_to_username = details["username"]

                await self.cache.add_message(
                    chat_id=event.chat.id,
                    message_id=event.message_id,
                    user_id=user_id,
                    username=username,
                    text=text,
                    timestamp=event.date.timestamp(),
                    reply_to_text=reply_to_text,
                    reply_to_username=reply_to_username,
                )

        return await handler(event, data)

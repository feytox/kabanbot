from typing import Callable, Dict, Any, Awaitable
from aiogram import BaseMiddleware
from aiogram.types import Message, TelegramObject
from kabanbot.services.cache import MessageCache


class CacheMiddleware(BaseMiddleware):
    def __init__(self, cache: MessageCache):
        super().__init__()
        self.cache = cache

    async def __call__(
        self,
        handler: Callable[[TelegramObject, Dict[str, Any]], Awaitable[Any]],
        event: TelegramObject,
        data: Dict[str, Any],
    ) -> Any:
        if isinstance(event, Message):
            # Only cache text messages for now, or captions?
            # Requirement says "reads every new incoming message".
            # Storing text content is primary for LLM.
            text = event.text or event.caption or ""

            # We only care about group messages (filtering happens in router, but middleware sees all if attached globally)
            # But let's assume we attach this to the group router.
            if text:
                user_id = event.from_user.id if event.from_user else 0
                username = (
                    event.from_user.username or event.from_user.full_name or "Unknown"
                )

                await self.cache.add_message(
                    chat_id=event.chat.id,
                    message_id=event.message_id,
                    user_id=user_id,
                    username=username,
                    text=text,
                    timestamp=event.date.timestamp(),
                )

        return await handler(event, data)

from typing import Callable, Dict, Any, Awaitable
from aiogram import BaseMiddleware
from aiogram.types import Message, TelegramObject
from kabanbot.config import settings


class WhitelistMiddleware(BaseMiddleware):
    async def __call__(
        self,
        handler: Callable[[TelegramObject, Dict[str, Any]], Awaitable[Any]],
        event: TelegramObject,
        data: Dict[str, Any],
    ) -> Any:
        if not isinstance(event, Message):
            return await handler(event, data)

        # If whitelist is empty, we allow everyone (feature disabled)
        if not settings.ALLOWED_GROUPS:
            return await handler(event, data)

        if event.chat.id not in settings.ALLOWED_GROUPS:
            await event.reply("⛔ This group is not authorized to use this bot.")
            return

        return await handler(event, data)

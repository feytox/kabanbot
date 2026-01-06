from html import escape

from aiogram import Router, F, Bot
from aiogram.types import Message
from aiogram.filters import Command

from kabanbot.services.cache import MessageCache
from kabanbot.services.llm import LLMService
import telegramify_markdown

group_router = Router()
group_router.message.filter(F.chat.type.in_({"group", "supergroup"}))


@group_router.message(F.text.contains("@all"))
async def mention_all(message: Message, bot: Bot, cache: MessageCache):
    """Pings all non-bot users in the group when a message contains @all."""
    users: dict[int, str] = {}

    cached_users = await cache.get_unique_users(message.chat.id)
    for user in cached_users:
        users[user["user_id"]] = user["username"]

    try:
        async for member in bot.get_chat_administrators(message.chat.id):
            user = member.user
            if not user.is_bot and user.id not in users:
                users[user.id] = user.full_name
    except Exception:
        pass

    if message.from_user:
        users.pop(message.from_user.id, None)

    if not users:
        await message.reply("Не удалось найти пользователей для упоминания.")
        return

    mentions = [
        f'<a href="tg://user?id={uid}">@{escape(name)}</a>'
        for uid, name in users.items()
    ]
    await message.reply(" ".join(mentions), parse_mode="HTML")


@group_router.message(Command("summary"), F.reply_to_message)
async def cmd_summary(message: Message, cache: MessageCache, llm: LLMService):
    """
    Summarizes the conversation starting from the replied-to message.
    """
    start_message = message.reply_to_message

    history = await cache.get_messages_since(message.chat.id, start_message.message_id)

    if not history:
        await message.reply(
            "I don't have enough history stored to summarize starting from that message, or no messages have appeared since then."
        )
        return

    processing_msg = await message.reply("Generating summary...")

    summary = await llm.summarize(history)

    formatted_summary = telegramify_markdown.markdownify(summary)

    await processing_msg.edit_text(formatted_summary, parse_mode="MarkdownV2")

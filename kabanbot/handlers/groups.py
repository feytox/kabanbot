from aiogram import Router, F
from aiogram.types import Message
from aiogram.filters import Command
from kabanbot.services.cache import MessageCache
from kabanbot.services.llm import LLMService
import telegramify_markdown

group_router = Router()
group_router.message.filter(F.chat.type.in_({"group", "supergroup"}))


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

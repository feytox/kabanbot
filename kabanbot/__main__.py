import asyncio
import logging
from aiogram import Bot, Dispatcher
from aiogram.client.default import DefaultBotProperties
from aiogram.enums import ParseMode

from kabanbot.config import settings
from kabanbot.services.cache import MessageCache
from kabanbot.services.llm import LLMService
from kabanbot.handlers.groups import group_router
from kabanbot.middlewares.cache import CacheMiddleware


async def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s - %(levelname)s - %(name)s - %(message)s",
    )

    # Initialize Services
    cache = MessageCache(db_path=settings.DB_PATH, max_size=settings.CACHE_SIZE)
    await cache.initialize()

    llm = LLMService()

    # Initialize Bot and Dispatcher
    bot = Bot(
        token=settings.BOT_TOKEN,
        default=DefaultBotProperties(parse_mode=ParseMode.HTML),
    )
    dp = Dispatcher()

    # Register Middleware
    # We pass cache to middleware
    group_router.message.outer_middleware(CacheMiddleware(cache))

    # Register Router
    dp.include_router(group_router)

    # DI for Handlers
    # We inject services into workflow_data so handlers can access them
    dp["cache"] = cache
    dp["llm"] = llm

    logging.info("Starting bot...")
    await dp.start_polling(bot)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        logging.info("Bot stopped.")

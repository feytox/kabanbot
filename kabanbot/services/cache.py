import aiosqlite
import logging
import asyncio
from typing import List, Dict, Any

logger = logging.getLogger(__name__)


class MessageCache:
    def __init__(self, db_path: str, max_size: int = 1000):
        self.db_path = db_path
        self.max_size = max_size
        self._lock = asyncio.Lock()

    async def initialize(self):
        """Initializes the database table."""
        import os

        os.makedirs(os.path.dirname(os.path.abspath(self.db_path)), exist_ok=True)

        async with aiosqlite.connect(self.db_path) as db:
            await db.execute("""
                CREATE TABLE IF NOT EXISTS messages (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    chat_id INTEGER,
                    message_id INTEGER,
                    user_id INTEGER,
                    username TEXT,
                    text TEXT,
                    timestamp REAL,
                    reply_to_text TEXT,
                    reply_to_username TEXT
                )
            """)
            await db.execute(
                "CREATE INDEX IF NOT EXISTS idx_chat_timestamp ON messages (chat_id, timestamp)"
            )

            existing_columns = set()
            async with db.execute("PRAGMA table_info(messages)") as cursor:
                async for row in cursor:
                    existing_columns.add(row[1])

            if "reply_to_text" not in existing_columns:
                await db.execute("ALTER TABLE messages ADD COLUMN reply_to_text TEXT")
                logger.info("Migrated database: added reply_to_text column")

            if "reply_to_username" not in existing_columns:
                await db.execute(
                    "ALTER TABLE messages ADD COLUMN reply_to_username TEXT"
                )
                logger.info("Migrated database: added reply_to_username column")

            await db.commit()

    async def add_message(
        self,
        chat_id: int,
        message_id: int,
        user_id: int,
        username: str,
        text: str,
        timestamp: float,
        reply_to_text: str | None = None,
        reply_to_username: str | None = None,
    ):
        """Adds a message to the cache and prunes old ones."""
        async with self._lock:
            async with aiosqlite.connect(self.db_path) as db:
                await db.execute(
                    """
                    INSERT INTO messages (chat_id, message_id, user_id, username, text, timestamp, reply_to_text, reply_to_username)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                    (
                        chat_id,
                        message_id,
                        user_id,
                        username,
                        text,
                        timestamp,
                        reply_to_text,
                        reply_to_username,
                    ),
                )
                await db.commit()

                await self._prune(db, chat_id)

    async def _prune(self, db: aiosqlite.Connection, chat_id: int):
        """Deletes messages exceeding the max_size for the given chat."""
        # Check count
        async with db.execute(
            "SELECT count(*) FROM messages WHERE chat_id = ?", (chat_id,)
        ) as cursor:
            row = await cursor.fetchone()
            count = row[0] if row else 0

        if count > self.max_size:
            diff = count - self.max_size
            # Delete the oldest 'diff' messages
            # Nested query to find IDs of oldest messages
            await db.execute(
                """
                DELETE FROM messages 
                WHERE id IN (
                    SELECT id FROM messages 
                    WHERE chat_id = ? 
                    ORDER BY timestamp ASC 
                    LIMIT ?
                )
            """,
                (chat_id, diff),
            )
            await db.commit()
            logger.debug(f"Pruned {diff} messages for chat {chat_id}")

    async def get_messages_since(
        self, chat_id: int, since_message_id: int
    ) -> List[Dict[str, Any]]:
        """Retrieves messages in a chat that occurred after the given message_id."""
        async with aiosqlite.connect(self.db_path) as db:
            # First find the timestamp of the reference message
            async with db.execute(
                "SELECT timestamp FROM messages WHERE chat_id = ? AND message_id = ?",
                (chat_id, since_message_id),
            ) as cursor:
                row = await cursor.fetchone()
                if not row:
                    return []  # Reference message not found in cache
                ref_timestamp = row[0]

            async with db.execute(
                """
                SELECT username, text, timestamp, message_id, reply_to_text, reply_to_username
                FROM messages 
                WHERE chat_id = ? AND timestamp >= ?
                ORDER BY timestamp ASC
            """,
                (chat_id, ref_timestamp),
            ) as cursor:
                rows = await cursor.fetchall()

                return [
                    {
                        "username": r[0],
                        "text": r[1],
                        "timestamp": r[2],
                        "message_id": r[3],
                        "reply_to_text": r[4],
                        "reply_to_username": r[5],
                    }
                    for r in rows
                ]

    async def get_message_details(
        self, chat_id: int, message_id: int
    ) -> Dict[str, Any] | None:
        """Retrieves text and username for a specific message."""
        async with aiosqlite.connect(self.db_path) as db:
            async with db.execute(
                "SELECT text, username FROM messages WHERE chat_id = ? AND message_id = ?",
                (chat_id, message_id),
            ) as cursor:
                row = await cursor.fetchone()
                if row:
                    return {"text": row[0], "username": row[1]}
                return None

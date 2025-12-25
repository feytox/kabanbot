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
                    timestamp REAL
                )
            """)
            await db.execute(
                "CREATE INDEX IF NOT EXISTS idx_chat_timestamp ON messages (chat_id, timestamp)"
            )
            await db.commit()

    async def add_message(
        self,
        chat_id: int,
        message_id: int,
        user_id: int,
        username: str,
        text: str,
        timestamp: float,
    ):
        """Adds a message to the cache and prunes old ones."""
        async with self._lock:  # Ensure sequential writes/prunes per instance
            async with aiosqlite.connect(self.db_path) as db:
                await db.execute(
                    """
                    INSERT INTO messages (chat_id, message_id, user_id, username, text, timestamp)
                    VALUES (?, ?, ?, ?, ?, ?)
                """,
                    (chat_id, message_id, user_id, username, text, timestamp),
                )
                await db.commit()

                # Prune
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

            # Get messages strictly after that timestamp
            async with db.execute(
                """
                SELECT username, text, timestamp, message_id 
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
                    }
                    for r in rows
                ]

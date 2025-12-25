from litellm import acompletion
import logging
from typing import List, Dict, Any
from kabanbot.config import settings
from kabanbot.prompts import SYSTEM_PROMPT

logger = logging.getLogger(__name__)


class LLMService:
    def __init__(self):
        self.system_prompt = SYSTEM_PROMPT

    async def summarize(self, messages: List[Dict[str, Any]]) -> str:
        """
        Summarizes a list of messages using the configured LLM.

        Args:
            messages: List of dicts with keys 'username', 'text', 'timestamp'.

        Returns:
            The summary string.
        """
        if not messages:
            return "No messages to summarize."

        # Format the conversation history
        conversation_text = "\n".join(
            f"{msg['username']}: {msg['text']}" for msg in messages
        )

        try:
            response = await acompletion(
                model=settings.LLM_MODEL,
                api_key=settings.LLM_API_KEY,
                base_url=settings.LLM_BASE_URL,
                messages=[
                    {"role": "system", "content": self.system_prompt},
                    {
                        "role": "user",
                        "content": f"Please summarize the following conversation:\n\n```\n{conversation_text}\n```",
                    },
                ],
            )
            return response.choices[0].message.content
        except Exception as e:
            logger.error(f"Error calling LLM: {e}")
            return "Sorry, I couldn't generate a summary at this time."

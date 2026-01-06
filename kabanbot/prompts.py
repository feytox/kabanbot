from pathlib import Path

BASE_DIR = Path(__file__).resolve().parent.parent
PROMPTS_DIR = BASE_DIR / "prompts"


def _load_prompt(filename: str) -> str:
    """Load a prompt from the prompts directory."""
    file_path = PROMPTS_DIR / filename
    if file_path.exists():
        return file_path.read_text(encoding="utf-8").strip()
    return ""


SYSTEM_PROMPT = _load_prompt("summary.md")

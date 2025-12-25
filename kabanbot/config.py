from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8")

    BOT_TOKEN: str

    # LLM Settings
    LLM_API_KEY: str | None = None
    LLM_BASE_URL: str | None = None
    LLM_MODEL: str | None = None

    # Database Settings
    DB_PATH: str = "data/messages.db"

    # Cache Settings
    CACHE_SIZE: int = 1000


settings = Settings()

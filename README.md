# Kabanbot

A Go-based Telegram bot featuring LLM integration. This bot is written in idiomatic, modular Go, utilizing the latest Telegram Bot API and supporting generic LLM providers via OpenAI-compatible endpoints.

## Features

- **Telegram Bot API:** Utilizes the lightweight and zero-dependency `github.com/go-telegram/bot` framework.
- **Unified LLM Service:** Integrates with any OpenAI-compatible LLM endpoint (including OpenAI, DeepSeek, OpenRouter, Ollama, or LiteLLM proxy).
- **Dynamic Start Greeting:** Generates custom welcoming messages for users using LLM completions on `/start`.

## Prerequisites

- **Go:** 1.22 or higher.
- **Docker & Docker Compose** (optional, for containerized execution).

## Local Development

### 1. Setup Environment
Clone/navigate to the project and copy the environment template or create a `.env` file:
```env
BOT_TOKEN=your_telegram_bot_token
LLM_API_KEY=your_llm_api_key
LLM_BASE_URL=https://api.openai.com/v1
LLM_MODEL=gpt-4o-mini
```

### 2. Install Dependencies
Download required modules:
```bash
go mod download
```

### 3. Run Locally
Start the bot directly:
```bash
go run main.go
```

### 4. Run Unit Tests
Unit tests are located in the dedicated `tests/` directory:
```bash
go test ./...
```

---

## Containerized Deployment

### With Docker
Build the docker image:
```bash
docker build -t kabanbot .
```

### With Docker Compose
To build and run the service in the background:
```bash
docker compose up -d --build
```

# Telegram Personal Finance Assistant

A modern, containerized monorepo containing a Telegram personal finance assistant named **Hermes** powered by Go, Python FastAPI, and Gemini/OpenAI-compatible LLMs.

---

## 🌟 Key Features

* **AI-Powered Transaction Entry**: Just text the bot in casual Indonesian (e.g., `beli nasi padang 25rb`, `gaji masuk 5jt`) and Hermes will automatically parse the type, amount, category, and description, and save it in Postgres.
* **Conversational Context Memory**: Stored locally in a Postgres table, keeping the **last 20 messages per user**. This allows multi-turn follow-ups such as:
  * User: `"makan siang 30rb, kopi 20rb"`
  * User: `"berapa total tadi?"`
  * Bot: `"Totalnya Rp 50.000, bro!"`
  * User: `"yang paling besar apa?"`
  * Bot: `"Yang paling besar tadi makan siang sebesar Rp 30.000, bro."`
* **Budget Monitoring & Over-Budget Warnings**:
  * Set total monthly budget limit (e.g., `"set budget bulanan gua 5 juta"`).
  * Set specific category limits (e.g., `"set limit food 500rb"`).
  * **Proactive Warning alerts**: Immediately notifies you if your new transaction breaches the monthly or category limits.
* **Dynamic LLM Configuration**:
  * Run on Gemini using `gemini-1.5-flash` or `gemini-1.5-pro` (Gemini Pro).
  * Run on **any other OpenAI-compatible LLM** (e.g., local Ollama, LM Studio, OpenAI API) by configuring base URL settings in environment variables.

---

## 📂 Project Structure

```
finance-bot/
├── bot/                # Go Telegram bot service
│   ├── config/         # Environment variable configuration loading
│   ├── handlers/       # Telegram bot handlers & reply card formatting
│   ├── llm/            # Hermes client (OpenAI-compatible) and tool registries
│   ├── models/         # Postgres models (User, Transaction, Report, ChatMessage)
│   ├── repositories/   # DB query handlers (Postgres, DDL migration scripts)
│   ├── services/       # Service layer (AI Client, Orchestrator, FinanceService)
│   └── Dockerfile      # Multi-stage Docker build
├── ai-service/         # Python FastAPI service (AI logic)
│   ├── parser_service.py # Gemini transaction parsing and monthly tips generation
│   ├── main.py         # FastAPI application and health endpoint
│   ├── requirements.txt
│   └── Dockerfile      # Python slim runner
├── db/                 # Database configuration
│   ├── migrations/     # Database initialization schema (init.up.sql)
│   └── seed.sql        # Database seed data
├── docker-compose.yml  # Main local orchestration
├── .env.example        # Environment variables template
├── Makefile            # Developer helper scripts
└── README.md           # This documentation
```

---

## 🚀 Quick Start

### Prerequisites
Ensure you have the following installed:
* Docker and Docker Compose
* `make` (optional, for running convenience commands)

### Running Locally

1. **Set up Environment Variables**:
   Copy `.env.example` to `.env` and fill in your keys:
   ```bash
   make setup
   ```
   Modify `.env` to configure your Telegram Token and API keys:
   * `TELEGRAM_BOT_TOKEN`: Token obtained from BotFather.
   * `GEMINI_API_KEY`: Google AI Studio API key.

2. **Build and Run Services**:
   ```bash
   make build
   make up
   ```

3. **Verify Service Health**:
   Check if all containers started properly:
   ```bash
   make status
   ```
   Or query health status directly:
   * **FastAPI AI Service**: [http://localhost:8000/health](http://localhost:8000/health)
   * **Go Bot Service**: [http://localhost:8080/health](http://localhost:8080/health)

4. **Shutdown and Clean Volumes**:
   ```bash
   make down
   ```

---

## 🛠️ Configuration Details (`.env`)

| Variable | Description | Default / Example |
| :--- | :--- | :--- |
| `TELEGRAM_BOT_TOKEN` | Token for Telegram Bot integration. | `8710277279:...` |
| `GEMINI_API_KEY` | Google Gemini API key. | `AQ.Ab8R...` |
| `GEMINI_MODEL` | Gemini model to use in the Python AI Service. | `gemini-1.5-pro` |
| `LLM_BASE_URL` | Base URL for custom LLM (OpenAI-compatible). | `https://api.openai.com/v1` |
| `LLM_MODEL` | Model identifier for custom LLM. | `gpt-4o` |
| `LLM_API_KEY` | API Key for custom LLM. | `sk-proj-...` |

---

## 🧬 Services Architecture

* **Go Bot Service (`bot`)**: Handles Telegram client updates, parses commands, writes to database repositories, routes intent analysis to the LLM client, and formats the user cards.
* **FastAPI AI Service (`ai-service`)**: Integrates with Gemini for unstructured transaction text parsing (`/parse`) and generating deep monthly financial analysis/savings observations (`/analyze`).
* **Postgres Database (`db`)**: Relational database storage for users, transaction registries, category budgets, and chat message history.

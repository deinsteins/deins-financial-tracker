# Telegram Personal Finance Assistant (Hermes)

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
* **Timezone-Aware Calculations**: Daily and monthly transaction query boundaries are dynamically mapped to your configured local timezone (e.g. `Asia/Jakarta`, UTC+7) instead of default server/UTC time.
* **Budget Monitoring & Alerts**:
  * Set specific category limits (e.g., `/budget set food 500rb`).
  * Check status using `/budget status` to see a structured usage card.
  * **Proactive Warning alerts**: Immediately notifies you if your transaction breaches `80%` (Warning) or `100%` (Over-budget limit) of category or monthly limits.
* **Financial Goals**:
  * Set saving goals with `/goal add <name> <amount> <deadline>`.
  * Track progress with `/goal status` which uses a **waterfall allocation** algorithm from your net savings and calculates required monthly saving rates to meet deadlines.
* **Multi-Wallet Support**:
  * Seeding default wallets: `cash`, `bank`, and `ewallet`.
  * Dynamic prefixes support (e.g., `"bca makan 25rb"`, `"ovo ojek 15k"`). Wallet prefixes are stripped to keep LLM parsing clean.
  * Automatically deducts or increments the specific wallet's balance. Check saldos using `/wallets` or `/wallet`.
* **Recurring Expense/Subscription Detection**:
  * Query using `/subscriptions` to check recurring expenses.
  * Auto-detects cycles matching **Weekly**, **Monthly**, or **Yearly** patterns from your history, estimating due dates and upcoming costs.
* **Upgraded Spending Analysis**:
  * Calling `/analyze` returns a rich, detailed dashboard highlighting:
    * **Financial Score (0-100)**: Scaled against your monthly saving rate.
    * **Highest spending day**: Formatted in Indonesian days/months.
    * **Spending anomalies**: Outliers > 2.5x of average transaction values or > Rp 500.000.
    * **Wasteful spending**: High frequency small items (e.g., repeated food/coffee purchases).
    * **Trends**: Dominating category spending trends.
    * **Saving recommendations**.
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
│   ├── models/         # Postgres models (User, Transaction, Report, ChatMessage, Goal, Wallet)
│   ├── repositories/   # DB query handlers (Postgres, DDL schemas)
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
├── docker-compose.yml  # Local developer environment composition
├── docker-compose.prod.yml # Production-ready composition (log rotation, strict network isolation)
├── .env.example        # Environment variables template
├── .env.prod.example   # Production environment template
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
   ```
   ```bash
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

## 🚀 Production Deployment & CI/CD

### Production Composition
The application contains a dedicated `docker-compose.prod.yml` configuration:
* **Log Rotation**: Captures up to 3 files of 10MB each.
* **Network Isolation**: Placed on isolated network drivers.
* **Port Bindings**: Ports bind locally (`127.0.0.1`) to restrict external access.
* **No Seeding**: Seed SQL files are blocked from mounting in production.

Production CLI:
* `make prod-build`: Build production Docker images.
* `make prod-up`: Spin up production stack.
* `make prod-down`: Stop production stack.
* `make prod-logs`: Tail logs.
* `make prod-status`: Check status.

### CI/CD Pipelines
Workflow pipelines are integrated under `.github/workflows/`:
1. **CI Pipeline (`ci.yml`)**: Compiles/tests Go services, checks Python FastAPI syntax, and verifies Docker Compose files on every pull request or commit to `master`.
2. **CD Pipeline (`cd.yml`)**: Deploys the code semi-automatically via manual trigger (`workflow_dispatch`) or tags matching `v*` directly to an AWS EC2 VPS:
   * Cleans older conflicting container names.
   * Pulls and performs rolling updates.
   * **Automated Rollback**: Validates VPS health after 15 seconds. If healthchecks fail or services crash, automatically rolls back to the previous stable git commit and restarts the stack.

---

## 🛠️ Configuration Details (`.env`)

| Variable | Description | Default / Example |
| :--- | :--- | :--- |
| `TELEGRAM_BOT_TOKEN` | Token for Telegram Bot integration. | `8710277279:...` |
| `GEMINI_API_KEY` | Google Gemini API key. | `AQ.Ab8R...` |
| `GEMINI_MODEL` | Gemini model to use in the Python AI Service. | `gemini-1.5-flash` |
| `TZ` | Location timezone for local calculations. | `Asia/Jakarta` |
| `LLM_BASE_URL` | Base URL for custom LLM (OpenAI-compatible). | `https://api.openai.com/v1` |
| `LLM_MODEL` | Model identifier for custom LLM. | `gpt-4o` |
| `LLM_API_KEY` | API Key for custom LLM. | `sk-proj-...` |

---

## 🧬 Services Architecture

* **Go Bot Service (`bot`)**: Handles Telegram client updates, parses commands, writes to database repositories, routes intent analysis to the LLM client, and formats the user cards.
* **FastAPI AI Service (`ai-service`)**: Integrates with Gemini for unstructured transaction text parsing (`/parse`) and generating deep monthly financial analysis/savings observations (`/analyze`).
* **Postgres Database (`db`)**: Relational database storage for users, transaction registries, category budgets, goals, wallets, and chat message history.

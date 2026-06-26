# Telegram Personal Finance Assistant

A monorepo containing a Telegram personal finance assistant powered by AI.

## Project Structure

```
finance-bot/
├── bot/                # Go Telegram bot service
│   ├── main.go         # Entry point and health server
│   └── Dockerfile      # Multi-stage Docker build
├── ai-service/         # Python FastAPI service (AI logic)
│   ├── main.py         # FastAPI application and health endpoint
│   ├── requirements.txt
│   └── Dockerfile      # Python slim runner
├── db/                 # Database configuration
│   └── init.sql        # Database initialization schema
├── docker-compose.yml  # Main local orchestration
├── .env.example        # Environment variables template
├── Makefile            # Developer helper scripts
└── README.md           # This documentation
```

## Quick Start

### Prerequisites
Ensure you have the following installed:
- Docker and Docker Compose
- `make` (optional, for running convenience commands)

### Running Locally

1. **Set up Environment Variables**:
   Copy `.env.example` to `.env` (creates variables with default dev values):
   ```bash
   make setup
   ```

2. **Build and Run Services**:
   ```bash
   make build
   make up
   ```

3. **Verify the Health of Services**:
   Check if all containers are healthy:
   ```bash
   make status
   ```
   Or query the API endpoints:
   - **FastAPI AI Service**: [http://localhost:8000/health](http://localhost:8000/health)
   - **Go Bot Service**: [http://localhost:8080/health](http://localhost:8080/health)

4. **Shutdown and Clean Volumes**:
   ```bash
   make down
   ```

## Services Architecture

- **Go Bot Service (`bot`)**: Deals with Telegram API client interactions, command parsing, and database transactions. It also communicates with the AI service.
- **FastAPI AI Service (`ai-service`)**: Implements AI models/prompts, budget analysis, and categorization routines.
- **Postgres Database (`db`)**: Reliable storage for transactions, categories, and user sessions.

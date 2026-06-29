package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
	_ "time/tzdata" // Embed timezone database inside Go binary

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/config"
	"finance-bot/bot/handlers"
	"finance-bot/bot/llm"
	"finance-bot/bot/repositories"
	"finance-bot/bot/services"
)

var (
	botInitialized = false
	botInitError   string
	dbPool         *pgxpool.Pool
	dbPoolErr      error
)

func connectDatabase(cfg *config.Config) {
	connStr := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	log.Printf("Connecting to Postgres database using pgxpool at %s:%s...", cfg.DBHost, cfg.DBPort)

	// Attempt connection with retries
	for i := 1; i <= 5; i++ {
		var err error
		dbPool, err = pgxpool.New(context.Background(), connStr)
		if err == nil {
			dbPoolErr = dbPool.Ping(context.Background())
			if dbPoolErr == nil {
				log.Println("Successfully connected to the database with pgxpool!")
				return
			}
			err = dbPoolErr
		}
		log.Printf("Attempt %d: pgxpool connection failed: %v. Retrying in 2 seconds...", i, err)
		time.Sleep(2 * time.Second)
	}

	log.Printf("Warning: Could not establish a database connection with pgxpool after 5 attempts")
}

func main() {
	// Load configuration
	cfg := config.Load()
	log.Printf("Starting bot service in [%s] environment...", cfg.Env)

	// Connect to Database synchronously so repositories can be initialized
	// (or we can run connectDatabase synchronously to ensure pool exists, since it has built-in retries)
	connectDatabase(cfg)

	// Initialize repositories
	userRepo := repositories.NewPostgresUserRepository(dbPool)
	txRepo := repositories.NewPostgresTransactionRepository(dbPool)
	repRepo := repositories.NewPostgresReportRepository(dbPool)
	budgetRepo := repositories.NewPostgresBudgetRepository(dbPool)
	goalRepo := repositories.NewPostgresGoalRepository(dbPool)
	chatMemoryRepo := repositories.NewPostgresChatMemoryRepository(dbPool)

	// Initialize AI Client
	aiClient := services.NewAIClient(cfg.AIServiceURL)

	// Initialize services with repository injections
	financeSvc := services.NewFinanceService(aiClient, userRepo, txRepo, repRepo, budgetRepo, goalRepo, chatMemoryRepo)

	// Initialize Hermes LLM Client and Orchestration Service
	// Resolve LLM parameters with support for LLM_BASE_URL env setting
	var llmAPIURL string
	if cfg.LLMBaseURL != "" {
		llmAPIURL = cfg.LLMBaseURL
	} else {
		llmAPIURL = cfg.HermesAPIURL
	}

	var llmModel string
	if cfg.LLMModel != "" {
		llmModel = cfg.LLMModel
	} else {
		llmModel = cfg.HermesModel
	}

	var llmAPIKey string
	if cfg.LLMAPIKey != "" {
		llmAPIKey = cfg.LLMAPIKey
	} else if cfg.HermesAPIKey != "" {
		llmAPIKey = cfg.HermesAPIKey
	} else {
		llmAPIKey = cfg.GeminiAPIKey
	}

	registry := llm.NewRegistry()
	hermesLLMClient := llm.NewHermesClient(llm.ClientConfig{
		APIURL:   llmAPIURL,
		Model:    llmModel,
		APIKey:   llmAPIKey,
		Registry: registry,
	})
	orchestrationSvc := services.NewOrchestrationService(hermesLLMClient, registry, financeSvc)

	// Initialize Telegram Bot if token is present and not default placeholder
	var bot *tgbotapi.BotAPI
	var err error

	if cfg.TelegramToken == "" || cfg.TelegramToken == "YOUR_TELEGRAM_BOT_TOKEN_HERE" {
		botInitError = "Telegram Token is not configured or is the default placeholder. Bot listener is disabled."
		log.Println("WARNING:", botInitError)
	} else {
		bot, err = tgbotapi.NewBotAPI(cfg.TelegramToken)
		if err != nil {
			botInitError = err.Error()
			log.Printf("ERROR: Failed to initialize Telegram Bot: %v. Bot listener is disabled.", err)
		} else {
			botInitialized = true
			log.Printf("Authorized on account %s", bot.Self.UserName)

			// Start Long Polling in a separate goroutine
			u := tgbotapi.NewUpdate(0)
			u.Timeout = 60
			updates := bot.GetUpdatesChan(u)

			handler := handlers.NewBotHandler(bot, financeSvc, orchestrationSvc)
			go handler.HandleUpdates(updates)
			log.Println("Started Telegram long polling listener successfully.")
		}
	}

	// Start health endpoint server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		status := "ok"
		if !botInitialized {
			status = "degraded"
		}

		dbStatus := "connected"
		if dbPool == nil {
			dbStatus = "disconnected"
			status = "degraded"
		} else {
			pingCtx, pingCancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer pingCancel()
			if err := dbPool.Ping(pingCtx); err != nil {
				dbStatus = fmt.Sprintf("disconnected: %v", err)
				status = "degraded"
			}
		}

		response := map[string]interface{}{
			"status":          status,
			"service":         "telegram-bot-service",
			"time":            time.Now().Format(time.RFC3339),
			"bot_initialized": botInitialized,
			"bot_error":       botInitError,
			"database":        dbStatus,
			"environment":     cfg.Env,
		}
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Telegram Personal Finance Assistant - Go Bot Service. Active routes: /health",
		})
	})

	log.Printf("Starting health server on port %s...", cfg.BotPort)
	if err := http.ListenAndServe(":"+cfg.BotPort, mux); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

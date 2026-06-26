package config

import (
	"os"
)

type Config struct {
	TelegramToken string
	BotPort       string
	DBHost        string
	DBPort        string
	DBUser        string
	DBPassword    string
	DBName        string
	AIServiceURL  string
	Env           string
	HermesAPIURL  string
	HermesModel   string
	HermesAPIKey  string
	GeminiAPIKey  string
	LLMBaseURL    string
	LLMModel      string
	LLMAPIKey     string
}

func Load() *Config {
	return &Config{
		TelegramToken: getEnv("TELEGRAM_BOT_TOKEN", ""),
		BotPort:       getEnv("BOT_PORT", "8080"),
		DBHost:        getEnv("DB_HOST", "db"),
		DBPort:        getEnv("DB_PORT", "5432"),
		DBUser:        getEnv("DB_USER", "postgres"),
		DBPassword:    getEnv("DB_PASSWORD", "postgres_secure_pass"),
		DBName:        getEnv("DB_NAME", "finance_db"),
		AIServiceURL:  getEnv("AI_SERVICE_URL", "http://ai-service:8000"),
		Env:           getEnv("ENV", "development"),
		HermesAPIURL:  getEnv("HERMES_API_URL", ""),
		HermesModel:   getEnv("HERMES_MODEL", ""),
		HermesAPIKey:  getEnv("HERMES_API_KEY", ""),
		GeminiAPIKey:  getEnv("GEMINI_API_KEY", ""),
		LLMBaseURL:    getEnv("LLM_BASE_URL", ""),
		LLMModel:      getEnv("LLM_MODEL", ""),
		LLMAPIKey:     getEnv("LLM_API_KEY", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

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
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

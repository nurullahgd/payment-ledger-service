package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL        string
	RedisAddr          string
	WorkerCount        int
	Port               string
	RateLimitPerMinute int
}

func Load() *Config {
	_ = godotenv.Load()

	return &Config{
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://ledger_user:ledger_password@localhost:5433/ledger_db?sslmode=disable"),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		WorkerCount:        getEnvInt("WORKER_COUNT", 5),
		Port:               getEnv("PORT", "8080"),
		RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 60),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

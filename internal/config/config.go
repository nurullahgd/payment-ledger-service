package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	WorkerCount int
	Port        string
}

func Load() *Config {
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set in environment variables")
	}

	workerCount := 5
	if wc := os.Getenv("WORKER_COUNT"); wc != "" {
		if parsed, err := strconv.Atoi(wc); err == nil {
			workerCount = parsed
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{
		DatabaseURL: dbURL,
		WorkerCount: workerCount,
		Port:        port,
	}
}

package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/nurullahgd/payment-ledger-service/internal/handler"
	"github.com/nurullahgd/payment-ledger-service/internal/repository"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

func main() {
	_ = godotenv.Load()
	dbURL := getEnv("DATABASE_URL", "postgres://ledger_user:ledger_password@localhost:5433/ledger_db?sslmode=disable")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	port := getEnv("PORT", "8080")

	workerCountStr := getEnv("WORKER_COUNT", "5")
	workerCount, err := strconv.Atoi(workerCountStr)
	if err != nil || workerCount <= 0 {
		workerCount = 5
	}

	ctx := context.Background()

	log.Println("Connecting to PostgreSQL...")
	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()
	if err := dbPool.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Println("Connecting to Redis...")
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to ping redis: %v", err)
	}

	ledgerRepo := repository.NewLedgerRepository(dbPool)
	idempotencyRepo := repository.NewIdempotencyRepository(redisClient, 24*time.Hour)

	pool := worker.NewPool(workerCount, 1000, ledgerRepo)
	pool.Start(ctx)

	api := handler.NewAPI(pool, idempotencyRepo)
	router := api.Routes()

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	go func() {
		log.Printf("Server is starting on port %s...", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Listen and serve error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutdown signal received...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	pool.Stop()

	log.Println("Server exiting gracefully. Goodbye!")
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

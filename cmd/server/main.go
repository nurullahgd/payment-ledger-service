package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nurullahgd/payment-ledger-service/internal/config"
	"github.com/nurullahgd/payment-ledger-service/internal/handler"
	"github.com/nurullahgd/payment-ledger-service/internal/repository"
	"github.com/nurullahgd/payment-ledger-service/internal/service"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	log.Println("Connecting to PostgreSQL...")
	dbPool, err := repository.NewPostgresRepository(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	log.Println("Connecting to Redis...")
	redisCache, err := repository.NewRedisCache(ctx, cfg.RedisAddr, "", 0)
	if err != nil {
		log.Fatalf("Failed to connect to redis: %v", err)
	}
	defer redisCache.Client.Close()

	ledgerRepo := repository.NewLedgerRepository(dbPool)
	tenantRepo := repository.NewTenantRepository(dbPool)

	log.Println("Running database seed...")
	if err := repository.SeedData(ctx, dbPool, tenantRepo); err != nil {
		log.Fatalf("Failed to seed database: %v", err)
	}

	idempotencyRepo := repository.NewIdempotencyRepository(redisCache.Client, 24*time.Hour)
	ledgerService := service.NewLedgerService(tenantRepo, ledgerRepo)

	pool := worker.NewPool(cfg.WorkerCount, 1000, ledgerRepo)
	pool.Start(ctx)

	api := handler.NewAPI(pool, idempotencyRepo, ledgerService, tenantRepo, dbPool, redisCache)
	router := api.Routes()

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("Server starting on port %s...", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Listen error: %v", err)
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
	log.Println("Server exited gracefully.")
}

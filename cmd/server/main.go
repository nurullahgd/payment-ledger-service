package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nurullahgd/payment-ledger-service/internal/config"
	"github.com/nurullahgd/payment-ledger-service/internal/handler"
	"github.com/nurullahgd/payment-ledger-service/internal/repository"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Load()

	dbPool, err := repository.NewPostgresRepository(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	ledgerRepo := repository.NewLedgerRepository(dbPool)
	workerPool := worker.NewPool(cfg.WorkerCount, ledgerRepo)
	workerPool.Start(ctx)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: handler.NewAPI(workerPool).Routes(),
	}

	go func() {
		log.Println("Starting HTTP server on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen failed: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(ctxShutdown); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	workerPool.Stop()
	log.Println("Server exiting")
}

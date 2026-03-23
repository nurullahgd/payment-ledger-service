package repository_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nurullahgd/payment-ledger-service/internal/repository"
)

func setupIntegrationDB(t *testing.T) (*pgxpool.Pool, *repository.LedgerRepository, *repository.TenantRepository) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	pool, err := repository.NewPostgresRepository(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	t.Cleanup(func() { pool.Close() })

	return pool, repository.NewLedgerRepository(pool), repository.NewTenantRepository(pool)
}

func seedBalance(t *testing.T, pool *pgxpool.Pool, schemaName, merchantID string, balance int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), fmt.Sprintf(`
		INSERT INTO %s.balances (merchant_id, available_balance)
		VALUES ($1, $2)
		ON CONFLICT (merchant_id) DO UPDATE SET available_balance = $2
	`, schemaName), merchantID, balance)
	if err != nil {
		t.Fatalf("failed to seed balance: %v", err)
	}
}

func seedPendingTx(t *testing.T, pool *pgxpool.Pool, schemaName, ref, txType string, amount int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), fmt.Sprintf(`
		INSERT INTO %s.transactions (reference, type, amount, status, description)
		VALUES ($1, $2, $3, 'pending', 'integration test')
		ON CONFLICT (reference) DO NOTHING
	`, schemaName), ref, txType, amount)
	if err != nil {
		t.Fatalf("failed to seed transaction %s: %v", ref, err)
	}
}

// TestProcessTransaction_ParallelDebit_NoOverdraft proves that SELECT FOR UPDATE
// prevents overdraft even when multiple goroutines debit concurrently.
// Starting balance: 10000. Each debit: 1500. Expected successes: 6. Expected remainder: 1000.
func TestProcessTransaction_ParallelDebit_NoOverdraft(t *testing.T) {
	pool, ledgerRepo, tenantRepo := setupIntegrationDB(t)
	ctx := context.Background()

	merchantID := "integ_debit_test"
	schemaName := fmt.Sprintf("tenant_%s", merchantID)

	if err := tenantRepo.CreateTenantSchema(ctx, merchantID); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	const initialBalance = int64(10000)
	const debitAmount = int64(1500)
	const goroutines = 10

	seedBalance(t, pool, schemaName, merchantID, initialBalance)

	for i := 0; i < goroutines; i++ {
		seedPendingTx(t, pool, schemaName, fmt.Sprintf("ref-debit-%d", i), "debit", debitAmount)
	}

	var (
		wg      sync.WaitGroup
		success atomic.Int32
		failure atomic.Int32
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		ref := fmt.Sprintf("ref-debit-%d", i)
		go func(reference string) {
			defer wg.Done()
			if err := ledgerRepo.ProcessTransaction(ctx, merchantID, reference, "debit", debitAmount); err != nil {
				failure.Add(1)
			} else {
				success.Add(1)
			}
		}(ref)
	}

	wg.Wait()

	var finalBalance int64
	if err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT available_balance FROM %s.balances WHERE merchant_id = $1`, schemaName,
	), merchantID).Scan(&finalBalance); err != nil {
		t.Fatalf("failed to read final balance: %v", err)
	}

	expectedSuccesses := int32(initialBalance / debitAmount)
	if success.Load() != expectedSuccesses {
		t.Errorf("expected %d successful debits, got %d", expectedSuccesses, success.Load())
	}

	expectedFinal := initialBalance - int64(expectedSuccesses)*debitAmount
	if finalBalance != expectedFinal {
		t.Errorf("expected final balance %d, got %d", expectedFinal, finalBalance)
	}

	t.Logf("success=%d failure=%d finalBalance=%d", success.Load(), failure.Load(), finalBalance)
}

// TestProcessTransaction_ConcurrentCredit_BalanceConsistency proves that concurrent
// credits produce the exact sum with no lost updates.
func TestProcessTransaction_ConcurrentCredit_BalanceConsistency(t *testing.T) {
	pool, ledgerRepo, tenantRepo := setupIntegrationDB(t)
	ctx := context.Background()

	merchantID := "integ_credit_test"
	schemaName := fmt.Sprintf("tenant_%s", merchantID)

	if err := tenantRepo.CreateTenantSchema(ctx, merchantID); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	const initialBalance = int64(0)
	const creditAmount = int64(1000)
	const goroutines = 5

	seedBalance(t, pool, schemaName, merchantID, initialBalance)

	for i := 0; i < goroutines; i++ {
		seedPendingTx(t, pool, schemaName, fmt.Sprintf("ref-credit-%d", i), "credit", creditAmount)
	}

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		ref := fmt.Sprintf("ref-credit-%d", i)
		go func(reference string) {
			defer wg.Done()
			if err := ledgerRepo.ProcessTransaction(ctx, merchantID, reference, "credit", creditAmount); err != nil {
				t.Errorf("credit failed for ref %s: %v", reference, err)
			}
		}(ref)
	}

	wg.Wait()

	var finalBalance int64
	if err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT available_balance FROM %s.balances WHERE merchant_id = $1`, schemaName,
	), merchantID).Scan(&finalBalance); err != nil {
		t.Fatalf("failed to read final balance: %v", err)
	}

	expected := initialBalance + creditAmount*goroutines
	if finalBalance != expected {
		t.Errorf("expected final balance %d, got %d (lost update!)", expected, finalBalance)
	}

	t.Logf("5 concurrent credits of %d → finalBalance=%d", creditAmount, finalBalance)
}

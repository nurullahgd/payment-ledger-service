package repository

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func SeedData(ctx context.Context, db *pgxpool.Pool, tenantRepo *TenantRepository) error {
	publicSchemaSQL := `
	CREATE TABLE IF NOT EXISTS public.merchants (
		id VARCHAR(50) PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		api_key VARCHAR(100) UNIQUE NOT NULL,
		currency VARCHAR(3) NOT NULL,
		status VARCHAR(20) DEFAULT 'active',
		webhook_url TEXT,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);`

	if _, err := db.Exec(ctx, publicSchemaSQL); err != nil {
		return fmt.Errorf("failed to create public.merchants table: %w", err)
	}

	merchants := []struct {
		ID         string
		Name       string
		APIKey     string
		Currency   string
		WebhookURL string
	}{
		{"merchant_1", "Test Merchant One", "sk_test_12345", "USD", ""},
		{"merchant_2", "Test Merchant Two", "sk_test_67890", "EUR", ""},
	}

	for _, m := range merchants {
		insertSQL := `
		INSERT INTO public.merchants (id, name, api_key, currency, status, webhook_url)
		VALUES ($1, $2, $3, $4, 'active', NULLIF($5, ''))
		ON CONFLICT (id) DO NOTHING;`

		if _, err := db.Exec(ctx, insertSQL, m.ID, m.Name, m.APIKey, m.Currency, m.WebhookURL); err != nil {
			return fmt.Errorf("failed to insert merchant %s: %w", m.ID, err)
		}

		if err := tenantRepo.CreateTenantSchema(ctx, m.ID); err != nil {
			return fmt.Errorf("failed to create tenant schema for %s: %w", m.ID, err)
		}
	}

	if err := seedTransactions(ctx, db, "merchant_1"); err != nil {
		return fmt.Errorf("failed to seed transactions for merchant_1: %w", err)
	}
	if err := seedTransactions(ctx, db, "merchant_2"); err != nil {
		return fmt.Errorf("failed to seed transactions for merchant_2: %w", err)
	}

	log.Println("Seed data executed: test merchants, schemas and sample transactions are ready.")
	return nil
}

func seedTransactions(ctx context.Context, db *pgxpool.Pool, merchantID string) error {
	schema := fmt.Sprintf("tenant_%s", merchantID)

	var count int
	if err := db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.transactions`, schema)).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transactions := []struct {
		ref    string
		txType string
		amount int64
		status string
		desc   string
	}{
		{"seed-credit-001", "credit", 500000, "completed", "Initial deposit"},
		{"seed-credit-002", "credit", 150000, "completed", "Invoice payment received"},
		{"seed-debit-001", "debit", 75000, "completed", "Supplier payout"},
		{"seed-debit-002", "debit", 30000, "completed", "Service fee"},
		{"seed-credit-003", "credit", 200000, "pending", "Pending top-up"},
		{"seed-debit-003", "debit", 999999, "failed", "Overdraft attempt"},
	}

	for _, t := range transactions {
		insertTx := fmt.Sprintf(`
			INSERT INTO %s.transactions (reference, type, amount, status, description)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (reference) DO NOTHING`, schema)
		if _, err := tx.Exec(ctx, insertTx, t.ref, t.txType, t.amount, t.status, t.desc); err != nil {
			return fmt.Errorf("failed to insert seed transaction %s: %w", t.ref, err)
		}
	}

	finalBalance := int64(500000 + 150000 - 75000 - 30000)

	ledgerEntries := []struct {
		ref    string
		prev   int64
		next   int64
		change int64
	}{
		{"seed-credit-001", 0, 500000, 500000},
		{"seed-credit-002", 500000, 650000, 150000},
		{"seed-debit-001", 650000, 575000, -75000},
		{"seed-debit-002", 575000, 545000, -30000},
	}

	for _, e := range ledgerEntries {
		insertLedger := fmt.Sprintf(`
			INSERT INTO %s.ledger (transaction_ref, previous_balance, new_balance, change_amount)
			VALUES ($1, $2, $3, $4)`, schema)
		if _, err := tx.Exec(ctx, insertLedger, e.ref, e.prev, e.next, e.change); err != nil {
			return fmt.Errorf("failed to insert seed ledger entry: %w", err)
		}
	}

	updateBalance := fmt.Sprintf(`
		UPDATE %s.balances SET available_balance = $1, updated_at = NOW()
		WHERE merchant_id = $2`, schema)
	if _, err := tx.Exec(ctx, updateBalance, finalBalance, merchantID); err != nil {
		return fmt.Errorf("failed to update seed balance: %w", err)
	}

	return tx.Commit(ctx)
}

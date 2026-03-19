package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TenantRepository struct {
	db *pgxpool.Pool
}

func NewTenantRepository(db *pgxpool.Pool) *TenantRepository {
	return &TenantRepository{db: db}
}

func (r *TenantRepository) CreateTenantSchema(ctx context.Context, merchantID string) error {
	schemaName := fmt.Sprintf("tenant_%s", strings.ReplaceAll(merchantID, "-", "_"))

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && rbErr.Error() != "tx is closed" {
			log.Printf("unexpected rollback error: %v", rbErr)
		}
	}()

	if _, err := tx.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, schemaName)); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	txTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.transactions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			reference VARCHAR(255) UNIQUE NOT NULL,
			type VARCHAR(10) NOT NULL,
			amount BIGINT NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			description TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_transactions_status ON %s.transactions(status);
		CREATE INDEX IF NOT EXISTS idx_transactions_reference ON %s.transactions(reference);
	`, schemaName, schemaName, schemaName)
	if _, err := tx.Exec(ctx, txTableSQL); err != nil {
		return fmt.Errorf("failed to create transactions table: %w", err)
	}

	balanceTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.balances (
			merchant_id VARCHAR(50) PRIMARY KEY,
			available_balance BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
	`, schemaName)
	if _, err := tx.Exec(ctx, balanceTableSQL); err != nil {
		return fmt.Errorf("failed to create balances table: %w", err)
	}

	ledgerTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.ledger (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			transaction_ref VARCHAR(255) NOT NULL,
			previous_balance BIGINT NOT NULL,
			new_balance BIGINT NOT NULL,
			change_amount BIGINT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_ledger_ref ON %s.ledger(transaction_ref);
	`, schemaName, schemaName)
	if _, err := tx.Exec(ctx, ledgerTableSQL); err != nil {
		return fmt.Errorf("failed to create ledger table: %w", err)
	}

	initBalanceSQL := fmt.Sprintf(`
		INSERT INTO %s.balances (merchant_id, available_balance) 
		VALUES ($1, 0) ON CONFLICT DO NOTHING;
	`, schemaName)
	if _, err := tx.Exec(ctx, initBalanceSQL, merchantID); err != nil {
		return fmt.Errorf("failed to initialize balance: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit tenant schema creation: %w", err)
	}

	return nil
}

func (r *TenantRepository) GetBalance(ctx context.Context, merchantID string) (int64, string, error) {
	schemaName := fmt.Sprintf("tenant_%s", strings.ReplaceAll(merchantID, "-", "_"))

	var balance int64
	var currency string

	query := fmt.Sprintf(`
		SELECT b.available_balance, m.currency
		FROM %s.balances b
		JOIN public.merchants m ON m.id = $1
		WHERE b.merchant_id = $1
	`, schemaName)

	err := r.db.QueryRow(ctx, query, merchantID).Scan(&balance, &currency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "USD", nil
		}
		return 0, "", fmt.Errorf("failed to fetch balance: %w", err)
	}

	return balance, currency, nil
}

package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TenantRepository struct {
	db *pgxpool.Pool
}

func NewTenantRepository(db *pgxpool.Pool) *TenantRepository {
	return &TenantRepository{db: db}
}

func (r *TenantRepository) CreateTenantSchema(ctx context.Context, merchantID string) error {
	schemaName := fmt.Sprintf("tenant_%s", merchantID)

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	schemaSQL := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, schemaName)
	if _, err := tx.Exec(ctx, schemaSQL); err != nil {
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
		CREATE INDEX IF NOT EXISTS idx_status ON %s.transactions(status);
	`, schemaName, schemaName)
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
	`, schemaName)
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

package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LedgerRepository struct {
	db *pgxpool.Pool
}

func NewLedgerRepository(db *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{db: db}
}

func (r *LedgerRepository) ProcessTransaction(ctx context.Context, merchantID string, txRef string, txType string, amount int64) error {
	schemaName := fmt.Sprintf("tenant_%s", merchantID)

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Thread safe
	_, err = tx.Exec(ctx, `SELECT id FROM public.merchants WHERE id = $1 FOR UPDATE`, merchantID)
	if err != nil {
		return fmt.Errorf("failed to lock merchant: %w", err)
	}


	var previousBalance int64
	queryBalance := fmt.Sprintf(`
		SELECT new_balance FROM %s.ledger 
		ORDER BY created_at DESC LIMIT 1
	`, schemaName)

	err = tx.QueryRow(ctx, queryBalance).Scan(&previousBalance)
	if err != nil && err.Error() != "no rows in result set" {
		return fmt.Errorf("failed to query balance: %w", err)
	}

	newBalance := previousBalance
	var changeAmount int64

	if txType == "credit" {
		newBalance += amount
		changeAmount = amount
	} else if txType == "debit" {
		if previousBalance < amount {
			failSQL := fmt.Sprintf(`UPDATE %s.transactions SET status = 'failed' WHERE reference = $1`, schemaName)
			tx.Exec(ctx, failSQL, txRef)
			tx.Commit(ctx) 
			return fmt.Errorf("INSUFFICIENT_BALANCE: debit of %d exceeds available balance of %d", amount, previousBalance)
		}
		newBalance -= amount
		changeAmount = -amount
	}

	insertLedgerSQL := fmt.Sprintf(`
		INSERT INTO %s.ledger (transaction_ref, previous_balance, new_balance, change_amount)
		VALUES ($1, $2, $3, $4)
	`, schemaName)
	if _, err := tx.Exec(ctx, insertLedgerSQL, txRef, previousBalance, newBalance, changeAmount); err != nil {
		return fmt.Errorf("failed to insert ledger entry: %w", err)
	}

	updateTxSQL := fmt.Sprintf(`UPDATE %s.transactions SET status = 'completed' WHERE reference = $1`, schemaName)
	if _, err := tx.Exec(ctx, updateTxSQL, txRef); err != nil {
		return fmt.Errorf("failed to update transaction status: %w", err)
	}

	// 7. Her şey başarılıysa onayla (Commit)
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

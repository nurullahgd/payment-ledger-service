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

var ErrInsufficientBalance = errors.New("INSUFFICIENT_BALANCE")

type LedgerRepository struct {
	db *pgxpool.Pool
}

func NewLedgerRepository(db *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{db: db}
}

func (r *LedgerRepository) ProcessTransaction(ctx context.Context, merchantID string, txRef string, txType string, amount int64) error {
	schemaName := fmt.Sprintf("tenant_%s", strings.ReplaceAll(merchantID, "-", "_"))

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && err.Error() != "tx is closed" {
			log.Printf("Beklenmeyen rollback hatası: %v", err)
		}
	}()

	var previousBalance int64
	queryBalance := fmt.Sprintf(`
		SELECT available_balance FROM %s.balances 
		WHERE merchant_id = $1 FOR UPDATE
	`, schemaName)

	err = tx.QueryRow(ctx, queryBalance, merchantID).Scan(&previousBalance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("balance record not found for merchant %s", merchantID)
		}
		return fmt.Errorf("failed to lock and query balance: %w", err)
	}

	newBalance := previousBalance
	var changeAmount int64

	if txType == "credit" {
		newBalance += amount
		changeAmount = amount
	} else if txType == "debit" {
		if previousBalance < amount {
			failSQL := fmt.Sprintf(`UPDATE %s.transactions SET status = 'failed' WHERE reference = $1`, schemaName)
			if _, failErr := tx.Exec(ctx, failSQL, txRef); failErr != nil {
				return fmt.Errorf("failed to update tx status to failed: %w", failErr)
			}
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return fmt.Errorf("failed to commit failed transaction state: %w", commitErr)
			}
			return fmt.Errorf("%w: debit of %d exceeds available balance of %d", ErrInsufficientBalance, amount, previousBalance)
		}
		newBalance -= amount
		changeAmount = -amount
	}

	updateBalanceSQL := fmt.Sprintf(`
		UPDATE %s.balances SET available_balance = $1, updated_at = NOW() 
		WHERE merchant_id = $2
	`, schemaName)
	if _, err := tx.Exec(ctx, updateBalanceSQL, newBalance, merchantID); err != nil {
		return fmt.Errorf("failed to update balance: %w", err)
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

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

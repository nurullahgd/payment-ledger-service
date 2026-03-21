package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nurullahgd/payment-ledger-service/internal/domain"
)

var ErrInsufficientBalance = errors.New("INSUFFICIENT_BALANCE")

type LedgerRepository struct {
	db *pgxpool.Pool
}

func NewLedgerRepository(db *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{db: db}
}

func (r *LedgerRepository) InsertPendingTransaction(ctx context.Context, merchantID, reference, txType, description string, amount int64) (string, error) {
	schemaName := fmt.Sprintf("tenant_%s", strings.ReplaceAll(merchantID, "-", "_"))

	var txID string
	query := fmt.Sprintf(`
		INSERT INTO %s.transactions (reference, type, amount, status, description)
		VALUES ($1, $2, $3, 'pending', $4)
		RETURNING id
	`, schemaName)

	err := r.db.QueryRow(ctx, query, reference, txType, amount, description).Scan(&txID)
	if err != nil {
		return "", fmt.Errorf("failed to insert pending transaction: %w", err)
	}

	return txID, nil
}

func (r *LedgerRepository) GetTransactionByID(ctx context.Context, merchantID, txID string) (*domain.Transaction, error) {
	schemaName := fmt.Sprintf("tenant_%s", strings.ReplaceAll(merchantID, "-", "_"))

	query := fmt.Sprintf(`
		SELECT id, reference, type, amount, status, COALESCE(description, ''), created_at
		FROM %s.transactions
		WHERE id = $1
	`, schemaName)

	var tx domain.Transaction
	var status string

	err := r.db.QueryRow(ctx, query, txID).Scan(
		&tx.ID, &tx.Reference, &tx.Type, &tx.Amount, &status, &tx.Description, &tx.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get transaction by id: %w", err)
	}

	tx.Status = domain.TransactionStatus(status)
	return &tx, nil
}

func (r *LedgerRepository) ListTransactions(ctx context.Context, merchantID, statusFilter string, limit, offset int) ([]*domain.Transaction, int, error) {
	schemaName := fmt.Sprintf("tenant_%s", strings.ReplaceAll(merchantID, "-", "_"))

	args := []interface{}{limit, offset}
	whereClause := ""

	if statusFilter != "" {
		whereClause = "WHERE status = $3"
		args = append(args, statusFilter)
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM %s.transactions %s`, schemaName, whereClause)
	listQuery := fmt.Sprintf(`
		SELECT id, reference, type, amount, status, COALESCE(description, ''), created_at
		FROM %s.transactions %s
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, schemaName, whereClause)

	var total int
	if err := r.db.QueryRow(ctx, countQuery, args[2:]...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count transactions: %w", err)
	}

	rows, err := r.db.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list transactions: %w", err)
	}
	defer rows.Close()

	var txs []*domain.Transaction
	for rows.Next() {
		var tx domain.Transaction
		var status string
		if err := rows.Scan(&tx.ID, &tx.Reference, &tx.Type, &tx.Amount, &status, &tx.Description, &tx.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan transaction row: %w", err)
		}
		tx.Status = domain.TransactionStatus(status)
		txs = append(txs, &tx)
	}

	return txs, total, nil
}

func (r *LedgerRepository) ListLedgerEntries(ctx context.Context, merchantID string, limit, offset int) ([]*domain.LedgerEntry, int, error) {
	schemaName := fmt.Sprintf("tenant_%s", strings.ReplaceAll(merchantID, "-", "_"))

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM %s.ledger`, schemaName)
	listQuery := fmt.Sprintf(`
		SELECT id, transaction_ref, previous_balance, new_balance, change_amount, created_at
		FROM %s.ledger
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, schemaName)

	var total int
	if err := r.db.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count ledger entries: %w", err)
	}

	rows, err := r.db.Query(ctx, listQuery, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []*domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransactionRef, &e.PreviousBalance, &e.NewBalance, &e.ChangeAmount, &e.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan ledger row: %w", err)
		}
		entries = append(entries, &e)
	}

	return entries, total, nil
}

func (r *LedgerRepository) ProcessTransaction(ctx context.Context, merchantID string, txRef string, txType string, amount int64) error {
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

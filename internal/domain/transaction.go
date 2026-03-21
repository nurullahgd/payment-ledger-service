package domain

import "time"

type TransactionStatus string

const (
	TransactionStatusPending   TransactionStatus = "pending"
	TransactionStatusCompleted TransactionStatus = "completed"
	TransactionStatusFailed    TransactionStatus = "failed"
)

type Transaction struct {
	ID          string
	Reference   string
	Type        string
	Amount      int64
	Status      TransactionStatus
	Description string
	CreatedAt   time.Time
}

func (t *Transaction) IsTerminal() bool {
	return t.Status == TransactionStatusCompleted || t.Status == TransactionStatusFailed
}

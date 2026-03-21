package domain

import "time"

type LedgerEntry struct {
	ID              string
	TransactionRef  string
	PreviousBalance int64
	NewBalance      int64
	ChangeAmount    int64
	CreatedAt       time.Time
}

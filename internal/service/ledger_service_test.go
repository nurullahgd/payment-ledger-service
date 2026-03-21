package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nurullahgd/payment-ledger-service/internal/domain"
	"github.com/nurullahgd/payment-ledger-service/internal/service"
)

type mockBalanceGetter struct {
	balance  int64
	currency string
	err      error
}

func (m *mockBalanceGetter) GetBalance(_ context.Context, _ string) (int64, string, error) {
	return m.balance, m.currency, m.err
}

type mockTransactionRepository struct {
	txID        string
	transaction *domain.Transaction
	transactions []*domain.Transaction
	total       int
	entries     []*domain.LedgerEntry
	err         error
}

func (m *mockTransactionRepository) InsertPendingTransaction(_ context.Context, _, _, _, _ string, _ int64) (string, error) {
	return m.txID, m.err
}

func (m *mockTransactionRepository) GetTransactionByID(_ context.Context, _, _ string) (*domain.Transaction, error) {
	return m.transaction, m.err
}

func (m *mockTransactionRepository) ListTransactions(_ context.Context, _, _ string, _, _ int) ([]*domain.Transaction, int, error) {
	return m.transactions, m.total, m.err
}

func (m *mockTransactionRepository) ListLedgerEntries(_ context.Context, _ string, _, _ int) ([]*domain.LedgerEntry, int, error) {
	return m.entries, m.total, m.err
}

func TestGetCurrentBalance(t *testing.T) {
	tests := []struct {
		name         string
		balance      int64
		currency     string
		repoErr      error
		wantBalance  int64
		wantCurrency string
		wantErr      bool
	}{
		{name: "returns balance and currency", balance: 15000, currency: "USD", wantBalance: 15000, wantCurrency: "USD"},
		{name: "returns zero balance", balance: 0, currency: "EUR", wantBalance: 0, wantCurrency: "EUR"},
		{name: "propagates repository error", repoErr: errors.New("db error"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewLedgerService(
				&mockBalanceGetter{balance: tt.balance, currency: tt.currency, err: tt.repoErr},
				&mockTransactionRepository{},
			)

			balance, currency, err := svc.GetCurrentBalance(context.Background(), "merchant_1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if balance != tt.wantBalance {
				t.Errorf("balance: want %d, got %d", tt.wantBalance, balance)
			}
			if currency != tt.wantCurrency {
				t.Errorf("currency: want %s, got %s", tt.wantCurrency, currency)
			}
		})
	}
}

func TestCreatePendingTransaction(t *testing.T) {
	const mockID = "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name    string
		txID    string
		repoErr error
		wantErr bool
	}{
		{name: "creates pending transaction", txID: mockID},
		{name: "propagates repository error", repoErr: errors.New("unique constraint"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewLedgerService(
				&mockBalanceGetter{},
				&mockTransactionRepository{txID: tt.txID, err: tt.repoErr},
			)

			id, err := svc.CreatePendingTransaction(context.Background(), "merchant_1", "ref-001", "credit", "test", 1000)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if !tt.wantErr && id != tt.txID {
				t.Errorf("want id %q, got %q", tt.txID, id)
			}
		})
	}
}

func TestGetTransactionByID(t *testing.T) {
	mockTx := &domain.Transaction{ID: "tx-1", Reference: "ref-001", Status: domain.TransactionStatusPending}

	tests := []struct {
		name    string
		tx      *domain.Transaction
		repoErr error
		wantNil bool
		wantErr bool
	}{
		{name: "returns transaction", tx: mockTx},
		{name: "returns nil when not found", tx: nil, wantNil: true},
		{name: "propagates error", repoErr: errors.New("db error"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewLedgerService(
				&mockBalanceGetter{},
				&mockTransactionRepository{transaction: tt.tx, err: tt.repoErr},
			)

			tx, err := svc.GetTransactionByID(context.Background(), "merchant_1", "tx-1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if tt.wantNil && tx != nil {
				t.Error("expected nil transaction")
			}
			if !tt.wantNil && tx == nil {
				t.Error("expected non-nil transaction")
			}
		})
	}
}

func TestListTransactions(t *testing.T) {
	txList := []*domain.Transaction{
		{ID: "tx-1", Status: domain.TransactionStatusCompleted},
		{ID: "tx-2", Status: domain.TransactionStatusPending},
	}

	svc := service.NewLedgerService(
		&mockBalanceGetter{},
		&mockTransactionRepository{transactions: txList, total: 2},
	)

	result, total, err := svc.ListTransactions(context.Background(), "merchant_1", "", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("want total 2, got %d", total)
	}
	if len(result) != 2 {
		t.Errorf("want 2 transactions, got %d", len(result))
	}
}

func TestListLedgerEntries(t *testing.T) {
	entryList := []*domain.LedgerEntry{
		{ID: "le-1", TransactionRef: "ref-001", ChangeAmount: 1000, CreatedAt: time.Now()},
	}

	svc := service.NewLedgerService(
		&mockBalanceGetter{},
		&mockTransactionRepository{entries: entryList, total: 1},
	)

	result, total, err := svc.ListLedgerEntries(context.Background(), "merchant_1", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("want total 1, got %d", total)
	}
	if len(result) != 1 {
		t.Errorf("want 1 entry, got %d", len(result))
	}
}

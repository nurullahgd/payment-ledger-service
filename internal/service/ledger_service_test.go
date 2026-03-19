package service_test

import (
	"context"
	"errors"
	"testing"

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

type mockTransactionInserter struct {
	txID string
	err  error
}

func (m *mockTransactionInserter) InsertPendingTransaction(_ context.Context, _, _, _, _ string, _ int64) (string, error) {
	return m.txID, m.err
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
		{
			name:         "returns balance and currency",
			balance:      15000,
			currency:     "USD",
			wantBalance:  15000,
			wantCurrency: "USD",
		},
		{
			name:         "returns zero balance with currency",
			balance:      0,
			currency:     "EUR",
			wantBalance:  0,
			wantCurrency: "EUR",
		},
		{
			name:    "propagates repository error",
			repoErr: errors.New("db connection error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewLedgerService(
				&mockBalanceGetter{balance: tt.balance, currency: tt.currency, err: tt.repoErr},
				&mockTransactionInserter{},
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
	const mockTxID = "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name        string
		txID        string
		repoErr     error
		merchantID  string
		reference   string
		txType      string
		description string
		amount      int64
		wantErr     bool
	}{
		{
			name:        "creates pending transaction and returns id",
			txID:        mockTxID,
			merchantID:  "merchant_1",
			reference:   "ref-001",
			txType:      "credit",
			description: "Invoice #1042",
			amount:      1500,
		},
		{
			name:        "creates debit transaction",
			txID:        mockTxID,
			merchantID:  "merchant_2",
			reference:   "ref-002",
			txType:      "debit",
			description: "Refund",
			amount:      500,
		},
		{
			name:    "propagates repository error",
			repoErr: errors.New("unique constraint violation"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewLedgerService(
				&mockBalanceGetter{},
				&mockTransactionInserter{txID: tt.txID, err: tt.repoErr},
			)

			id, err := svc.CreatePendingTransaction(context.Background(), tt.merchantID, tt.reference, tt.txType, tt.description, tt.amount)

			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if id != tt.txID {
				t.Errorf("want txID %q, got %q", tt.txID, id)
			}
		})
	}
}

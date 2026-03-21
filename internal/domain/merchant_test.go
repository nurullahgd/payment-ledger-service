package domain_test

import (
	"testing"

	"github.com/nurullahgd/payment-ledger-service/internal/domain"
)

func TestMerchant_IsActive(t *testing.T) {
	tests := []struct {
		name   string
		status domain.MerchantStatus
		want   bool
	}{
		{"active merchant", domain.MerchantStatusActive, true},
		{"suspended merchant", domain.MerchantStatusSuspended, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &domain.Merchant{Status: tt.status}
			if got := m.IsActive(); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransaction_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status domain.TransactionStatus
		want   bool
	}{
		{"pending is not terminal", domain.TransactionStatusPending, false},
		{"completed is terminal", domain.TransactionStatusCompleted, true},
		{"failed is terminal", domain.TransactionStatusFailed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &domain.Transaction{Status: tt.status}
			if got := tx.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

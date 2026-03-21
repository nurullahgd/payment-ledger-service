package service

import (
	"context"

	"github.com/nurullahgd/payment-ledger-service/internal/domain"
)

type BalanceGetter interface {
	GetBalance(ctx context.Context, merchantID string) (int64, string, error)
}

type TransactionRepository interface {
	InsertPendingTransaction(ctx context.Context, merchantID, reference, txType, description string, amount int64) (string, error)
	GetTransactionByID(ctx context.Context, merchantID, txID string) (*domain.Transaction, error)
	ListTransactions(ctx context.Context, merchantID, statusFilter string, limit, offset int) ([]*domain.Transaction, int, error)
	ListLedgerEntries(ctx context.Context, merchantID string, limit, offset int) ([]*domain.LedgerEntry, int, error)
}

type LedgerService struct {
	balanceRepo BalanceGetter
	txRepo      TransactionRepository
}

func NewLedgerService(balanceRepo BalanceGetter, txRepo TransactionRepository) *LedgerService {
	return &LedgerService{
		balanceRepo: balanceRepo,
		txRepo:      txRepo,
	}
}

func (s *LedgerService) GetCurrentBalance(ctx context.Context, merchantID string) (int64, string, error) {
	return s.balanceRepo.GetBalance(ctx, merchantID)
}

func (s *LedgerService) CreatePendingTransaction(ctx context.Context, merchantID, reference, txType, description string, amount int64) (string, error) {
	return s.txRepo.InsertPendingTransaction(ctx, merchantID, reference, txType, description, amount)
}

func (s *LedgerService) GetTransactionByID(ctx context.Context, merchantID, txID string) (*domain.Transaction, error) {
	return s.txRepo.GetTransactionByID(ctx, merchantID, txID)
}

func (s *LedgerService) ListTransactions(ctx context.Context, merchantID, statusFilter string, limit, offset int) ([]*domain.Transaction, int, error) {
	return s.txRepo.ListTransactions(ctx, merchantID, statusFilter, limit, offset)
}

func (s *LedgerService) ListLedgerEntries(ctx context.Context, merchantID string, limit, offset int) ([]*domain.LedgerEntry, int, error) {
	return s.txRepo.ListLedgerEntries(ctx, merchantID, limit, offset)
}

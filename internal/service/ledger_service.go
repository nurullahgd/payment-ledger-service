package service

import "context"

type BalanceGetter interface {
	GetBalance(ctx context.Context, merchantID string) (int64, string, error)
}

type TransactionInserter interface {
	InsertPendingTransaction(ctx context.Context, merchantID, reference, txType, description string, amount int64) (string, error)
}

type LedgerService struct {
	balanceRepo BalanceGetter
	txRepo      TransactionInserter
}

func NewLedgerService(balanceRepo BalanceGetter, txRepo TransactionInserter) *LedgerService {
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

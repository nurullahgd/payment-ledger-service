package service

import (
	"context"

	"github.com/nurullahgd/payment-ledger-service/internal/repository"
)

type LedgerService struct {
	tenantRepo *repository.TenantRepository
}

func NewLedgerService(tr *repository.TenantRepository) *LedgerService {
	return &LedgerService{tenantRepo: tr}
}

func (s *LedgerService) GetCurrentBalance(ctx context.Context, merchantID string) (int64, string, error) {
	return s.tenantRepo.GetBalance(ctx, merchantID)
}

package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nurullahgd/payment-ledger-service/internal/domain"
	"github.com/nurullahgd/payment-ledger-service/internal/middleware"
)

type mockResolver struct {
	merchant *domain.Merchant
	err      error
}

func (m *mockResolver) GetMerchantByAPIKey(_ context.Context, _ string) (*domain.Merchant, error) {
	return m.merchant, m.err
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	m := middleware.MerchantFromContext(r.Context())
	if m == nil {
		http.Error(w, "no merchant in context", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func applyMiddleware(resolver middleware.MerchantResolver, apiKey string) *httptest.ResponseRecorder {
	handler := middleware.TenantMiddleware(resolver)(http.HandlerFunc(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestTenantMiddleware_MissingAPIKey(t *testing.T) {
	rr := applyMiddleware(&mockResolver{}, "")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestTenantMiddleware_InvalidAPIKey(t *testing.T) {
	rr := applyMiddleware(&mockResolver{merchant: nil}, "invalid-key")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestTenantMiddleware_SuspendedMerchant(t *testing.T) {
	suspended := &domain.Merchant{ID: "m1", Status: domain.MerchantStatusSuspended}
	rr := applyMiddleware(&mockResolver{merchant: suspended}, "sk_test_12345")
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestTenantMiddleware_ResolverError(t *testing.T) {
	rr := applyMiddleware(&mockResolver{err: errors.New("db down")}, "sk_test_12345")
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestTenantMiddleware_ActiveMerchant_InjectsContext(t *testing.T) {
	active := &domain.Merchant{ID: "merchant_1", Status: domain.MerchantStatusActive, Currency: "USD"}
	rr := applyMiddleware(&mockResolver{merchant: active}, "sk_test_12345")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

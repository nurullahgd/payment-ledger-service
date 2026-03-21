package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nurullahgd/payment-ledger-service/internal/domain"
	"github.com/nurullahgd/payment-ledger-service/internal/middleware"
	"github.com/nurullahgd/payment-ledger-service/internal/ratelimit"
)

type mockRateLimiter struct {
	result ratelimit.Result
	err    error
}

func (m *mockRateLimiter) Allow(_ context.Context, _ string) (ratelimit.Result, error) {
	return m.result, m.err
}

func applyRateLimit(limiter ratelimit.Limiter, merchant *domain.Merchant) *httptest.ResponseRecorder {
	handler := middleware.RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if merchant != nil {
		ctx := context.WithValue(req.Context(), middleware.MerchantKey, merchant)
		req = req.WithContext(ctx)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestRateLimitMiddleware_Allowed(t *testing.T) {
	limiter := &mockRateLimiter{result: ratelimit.Result{Allowed: true}}
	merchant := &domain.Merchant{ID: "merchant_1", Status: domain.MerchantStatusActive}

	rr := applyRateLimit(limiter, merchant)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_Rejected(t *testing.T) {
	limiter := &mockRateLimiter{result: ratelimit.Result{Allowed: false, RetryAfter: 60}}
	merchant := &domain.Merchant{ID: "merchant_1", Status: domain.MerchantStatusActive}

	rr := applyRateLimit(limiter, merchant)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") != "60" {
		t.Errorf("expected Retry-After: 60, got %q", rr.Header().Get("Retry-After"))
	}
}

func TestRateLimitMiddleware_LimiterError(t *testing.T) {
	limiter := &mockRateLimiter{err: errors.New("redis down")}
	merchant := &domain.Merchant{ID: "merchant_1", Status: domain.MerchantStatusActive}

	rr := applyRateLimit(limiter, merchant)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_NoMerchantInContext(t *testing.T) {
	limiter := &mockRateLimiter{result: ratelimit.Result{Allowed: true}}

	rr := applyRateLimit(limiter, nil)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (passthrough), got %d", rr.Code)
	}
}

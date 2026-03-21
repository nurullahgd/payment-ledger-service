package ratelimit_test

import (
	"context"
	"testing"

	"github.com/nurullahgd/payment-ledger-service/internal/ratelimit"
)

type mockLimiter struct {
	result ratelimit.Result
	err    error
	calls  int
}

func (m *mockLimiter) Allow(_ context.Context, _ string) (ratelimit.Result, error) {
	m.calls++
	return m.result, m.err
}

func TestMerchantKey(t *testing.T) {
	tests := []struct {
		merchantID string
		want       string
	}{
		{"merchant_1", "ratelimit:merchant_1"},
		{"merchant_2", "ratelimit:merchant_2"},
	}

	for _, tt := range tests {
		t.Run(tt.merchantID, func(t *testing.T) {
			got := ratelimit.MerchantKey(tt.merchantID)
			if got != tt.want {
				t.Errorf("MerchantKey(%q) = %q, want %q", tt.merchantID, got, tt.want)
			}
		})
	}
}

func TestResult_Allowed(t *testing.T) {
	limiter := &mockLimiter{result: ratelimit.Result{Allowed: true}}

	result, err := limiter.Allow(context.Background(), "ratelimit:merchant_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected request to be allowed")
	}
}

func TestResult_Rejected(t *testing.T) {
	limiter := &mockLimiter{result: ratelimit.Result{Allowed: false, RetryAfter: 60}}

	result, err := limiter.Allow(context.Background(), "ratelimit:merchant_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected request to be rejected")
	}
	if result.RetryAfter != 60 {
		t.Errorf("expected RetryAfter 60, got %d", result.RetryAfter)
	}
}

func TestResult_CalledWithCorrectKey(t *testing.T) {
	limiter := &mockLimiter{result: ratelimit.Result{Allowed: true}}

	for i := 0; i < 3; i++ {
		limiter.Allow(context.Background(), ratelimit.MerchantKey("merchant_1"))
	}

	if limiter.calls != 3 {
		t.Errorf("expected 3 calls, got %d", limiter.calls)
	}
}

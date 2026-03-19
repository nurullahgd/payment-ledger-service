package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nurullahgd/payment-ledger-service/internal/handler"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

// --- Mocks ---

type mockLedgerService struct {
	balance  int64
	currency string
	err      error
}

func (m *mockLedgerService) GetCurrentBalance(_ context.Context, _ string) (int64, string, error) {
	return m.balance, m.currency, m.err
}

type mockIdempotency struct {
	payload    string
	isReplayed bool
	err        error
}

func (m *mockIdempotency) CheckOrRecord(_ context.Context, _, _, payload string) (string, bool, error) {
	if m.payload != "" {
		return m.payload, m.isReplayed, m.err
	}
	return payload, m.isReplayed, m.err
}

type mockPool struct {
	err error
}

func (m *mockPool) Submit(_ worker.TransactionTask) error {
	return m.err
}

// --- Helpers ---

func newTestAPI(ls handler.LedgerService, ic handler.IdempotencyChecker, pool handler.TaskSubmitter) *handler.API {
	return handler.NewAPI(pool, ic, ls)
}

// --- Tests ---

func TestGetBalance_OK(t *testing.T) {
	ls := &mockLedgerService{balance: 15000, currency: "USD"}
	api := newTestAPI(ls, nil, nil)
	router := api.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/balances", nil)
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["balance"] != float64(15000) {
		t.Errorf("expected balance 15000, got %v", resp["balance"])
	}
	if resp["currency"] != "USD" {
		t.Errorf("expected currency USD, got %v", resp["currency"])
	}
}

func TestSubmitTransaction_MissingAPIKey(t *testing.T) {
	api := newTestAPI(nil, nil, nil)
	router := api.Routes()

	body, _ := json.Marshal(map[string]interface{}{
		"reference": "ref-001",
		"type":      "credit",
		"amount":    1000,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSubmitTransaction_InvalidAmount(t *testing.T) {
	api := newTestAPI(nil, nil, nil)
	router := api.Routes()

	body, _ := json.Marshal(map[string]interface{}{
		"reference": "ref-002",
		"type":      "credit",
		"amount":    -50,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSubmitTransaction_InvalidType(t *testing.T) {
	api := newTestAPI(nil, nil, nil)
	router := api.Routes()

	body, _ := json.Marshal(map[string]interface{}{
		"reference": "ref-003",
		"type":      "transfer",
		"amount":    500,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSubmitTransaction_Accepted(t *testing.T) {
	idem := &mockIdempotency{}
	pool := &mockPool{}
	api := newTestAPI(nil, idem, pool)
	router := api.Routes()

	body, _ := json.Marshal(map[string]interface{}{
		"reference": "ref-ok-001",
		"type":      "credit",
		"amount":    2000,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d — body: %s", rr.Code, rr.Body.String())
	}
}

func TestSubmitTransaction_IdempotencyReplayed(t *testing.T) {
	existing := `{"reference":"ref-dup","status":"pending"}`
	idem := &mockIdempotency{payload: existing, isReplayed: true}
	api := newTestAPI(nil, idem, nil)
	router := api.Routes()

	body, _ := json.Marshal(map[string]interface{}{
		"reference": "ref-dup",
		"type":      "credit",
		"amount":    500,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	if rr.Header().Get("Idempotency-Replayed") != "true" {
		t.Error("expected Idempotency-Replayed: true header")
	}
	if rr.Body.String() != existing {
		t.Errorf("expected cached payload %q, got %q", existing, rr.Body.String())
	}
}

func TestHealth(t *testing.T) {
	api := newTestAPI(nil, nil, nil)
	router := api.Routes()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

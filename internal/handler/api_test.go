package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nurullahgd/payment-ledger-service/internal/domain"
	"github.com/nurullahgd/payment-ledger-service/internal/handler"
	"github.com/nurullahgd/payment-ledger-service/internal/middleware"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

const mockTxID = "550e8400-e29b-41d4-a716-446655440000"

var activeMerchant = &domain.Merchant{
	ID:       "merchant_1",
	Status:   domain.MerchantStatusActive,
	Currency: "USD",
}

type mockResolver struct {
	merchant *domain.Merchant
	err      error
}

func (m *mockResolver) GetMerchantByAPIKey(_ context.Context, _ string) (*domain.Merchant, error) {
	return m.merchant, m.err
}

type mockLedgerService struct {
	balance  int64
	currency string
	txID     string
	err      error
}

func (m *mockLedgerService) GetCurrentBalance(_ context.Context, _ string) (int64, string, error) {
	return m.balance, m.currency, m.err
}

func (m *mockLedgerService) CreatePendingTransaction(_ context.Context, _, _, _, _ string, _ int64) (string, error) {
	return m.txID, m.err
}

type mockIdempotency struct {
	stored     map[string]string
	isReplayed bool
	getErr     error
	setErr     error
}

func (m *mockIdempotency) Get(_ context.Context, merchantID, reference string) (string, bool, error) {
	if m.getErr != nil {
		return "", false, m.getErr
	}
	key := merchantID + ":" + reference
	if v, ok := m.stored[key]; ok && m.isReplayed {
		return v, true, nil
	}
	return "", false, nil
}

func (m *mockIdempotency) Set(_ context.Context, merchantID, reference, payload string) error {
	if m.setErr != nil {
		return m.setErr
	}
	if m.stored == nil {
		m.stored = make(map[string]string)
	}
	m.stored[merchantID+":"+reference] = payload
	return nil
}

type mockPool struct {
	err error
}

func (m *mockPool) Submit(_ worker.TransactionTask) error {
	return m.err
}

func newTestAPI(ls handler.LedgerService, ic handler.IdempotencyChecker, pool handler.TaskSubmitter, resolver middleware.MerchantResolver) *handler.API {
	return handler.NewAPI(pool, ic, ls, resolver)
}

func newActiveResolver() *mockResolver {
	return &mockResolver{merchant: activeMerchant}
}

func TestGetBalance_OK(t *testing.T) {
	ls := &mockLedgerService{balance: 15000, currency: "USD"}
	api := newTestAPI(ls, nil, nil, newActiveResolver())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/balance", nil)
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

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

func TestGetBalance_ServiceError(t *testing.T) {
	ls := &mockLedgerService{err: errors.New("db error")}
	api := newTestAPI(ls, nil, nil, newActiveResolver())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/balance", nil)
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestGetBalance_MissingAPIKey(t *testing.T) {
	api := newTestAPI(nil, nil, nil, &mockResolver{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/balance", nil)
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestGetBalance_SuspendedMerchant(t *testing.T) {
	suspended := &domain.Merchant{ID: "m2", Status: domain.MerchantStatusSuspended}
	api := newTestAPI(nil, nil, nil, &mockResolver{merchant: suspended})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/balance", nil)
	req.Header.Set("X-API-Key", "sk_test_99999")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestSubmitTransaction_MissingAPIKey(t *testing.T) {
	api := newTestAPI(nil, nil, nil, &mockResolver{})

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-001", "type": "credit", "amount": 1000})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSubmitTransaction_InvalidAmount(t *testing.T) {
	api := newTestAPI(nil, nil, nil, newActiveResolver())

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-002", "type": "credit", "amount": -50})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSubmitTransaction_InvalidType(t *testing.T) {
	api := newTestAPI(nil, nil, nil, newActiveResolver())

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-003", "type": "transfer", "amount": 500})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSubmitTransaction_Accepted(t *testing.T) {
	idem := &mockIdempotency{}
	pool := &mockPool{}
	ls := &mockLedgerService{txID: mockTxID}
	api := newTestAPI(ls, idem, pool, newActiveResolver())

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-ok-001", "type": "credit", "amount": 2000, "description": "Invoice #100"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d — body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["id"] != mockTxID {
		t.Errorf("expected id %q, got %v", mockTxID, resp["id"])
	}
	if resp["status"] != "pending" {
		t.Errorf("expected status 'pending', got %v", resp["status"])
	}
}

func TestSubmitTransaction_IdempotencyReplayed(t *testing.T) {
	existingPayload := `{"id":"` + mockTxID + `","reference":"ref-dup","status":"pending"}`
	idem := &mockIdempotency{
		stored:     map[string]string{"merchant_1:ref-dup": existingPayload},
		isReplayed: true,
	}
	api := newTestAPI(nil, idem, nil, newActiveResolver())

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-dup", "type": "credit", "amount": 500})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	if rr.Header().Get("Idempotency-Replayed") != "true" {
		t.Error("expected Idempotency-Replayed: true header")
	}
	if rr.Body.String() != existingPayload {
		t.Errorf("expected cached payload %q, got %q", existingPayload, rr.Body.String())
	}
}

func TestSubmitTransaction_IdempotencyGetError(t *testing.T) {
	idem := &mockIdempotency{getErr: errors.New("redis down")}
	api := newTestAPI(nil, idem, nil, newActiveResolver())

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-005", "type": "credit", "amount": 100})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestSubmitTransaction_CreateTxError(t *testing.T) {
	idem := &mockIdempotency{}
	ls := &mockLedgerService{err: errors.New("db constraint")}
	api := newTestAPI(ls, idem, nil, newActiveResolver())

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-006", "type": "credit", "amount": 100})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestSubmitTransaction_QueueFull(t *testing.T) {
	idem := &mockIdempotency{}
	pool := &mockPool{err: errors.New("queue full")}
	ls := &mockLedgerService{txID: mockTxID}
	api := newTestAPI(ls, idem, pool, newActiveResolver())

	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-007", "type": "credit", "amount": 100})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sk_test_12345")
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
}

func TestHealth(t *testing.T) {
	api := newTestAPI(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	balance      int64
	currency     string
	txID         string
	transaction  *domain.Transaction
	transactions []*domain.Transaction
	entries      []*domain.LedgerEntry
	total        int
	err          error
}

func (m *mockLedgerService) GetCurrentBalance(_ context.Context, _ string) (int64, string, error) {
	return m.balance, m.currency, m.err
}

func (m *mockLedgerService) CreatePendingTransaction(_ context.Context, _, _, _, _ string, _ int64) (string, error) {
	return m.txID, m.err
}

func (m *mockLedgerService) GetTransactionByID(_ context.Context, _, _ string) (*domain.Transaction, error) {
	return m.transaction, m.err
}

func (m *mockLedgerService) ListTransactions(_ context.Context, _, _ string, _, _ int) ([]*domain.Transaction, int, error) {
	return m.transactions, m.total, m.err
}

func (m *mockLedgerService) ListLedgerEntries(_ context.Context, _ string, _, _ int) ([]*domain.LedgerEntry, int, error) {
	return m.entries, m.total, m.err
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

func newActiveResolver() *mockResolver {
	return &mockResolver{merchant: activeMerchant}
}

type mockHealthChecker struct {
	err error
}

func (m *mockHealthChecker) Ping(_ context.Context) error {
	return m.err
}

func newTestAPI(ls handler.LedgerService, ic handler.IdempotencyChecker, pool handler.TaskSubmitter, resolver middleware.MerchantResolver) *handler.API {
	return handler.NewAPI(pool, ic, ls, resolver, nil, nil)
}

func do(api *handler.API, method, path, apiKey string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rr := httptest.NewRecorder()
	api.Routes().ServeHTTP(rr, req)
	return rr
}

func TestHealth_OK(t *testing.T) {
	api := handler.NewAPI(nil, nil, nil, nil, &mockHealthChecker{}, &mockHealthChecker{})
	rr := do(api, http.MethodGet, "/health", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}
}

func TestHealth_DBDown(t *testing.T) {
	api := handler.NewAPI(nil, nil, nil, nil, &mockHealthChecker{err: errors.New("db down")}, &mockHealthChecker{})
	rr := do(api, http.MethodGet, "/health", "", nil)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestHealth_CacheDown(t *testing.T) {
	api := handler.NewAPI(nil, nil, nil, nil, &mockHealthChecker{}, &mockHealthChecker{err: errors.New("redis down")})
	rr := do(api, http.MethodGet, "/health", "", nil)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestGetBalance_OK(t *testing.T) {
	ls := &mockLedgerService{balance: 15000, currency: "USD"}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/balance", "sk_test_12345", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["balance"] != float64(15000) {
		t.Errorf("expected balance 15000, got %v", resp["balance"])
	}
	if resp["currency"] != "USD" {
		t.Errorf("expected currency USD, got %v", resp["currency"])
	}
}

func TestGetBalance_MissingAPIKey(t *testing.T) {
	rr := do(newTestAPI(nil, nil, nil, &mockResolver{}), http.MethodGet, "/api/v1/balance", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestGetBalance_SuspendedMerchant(t *testing.T) {
	suspended := &domain.Merchant{ID: "m2", Status: domain.MerchantStatusSuspended}
	rr := do(newTestAPI(nil, nil, nil, &mockResolver{merchant: suspended}), http.MethodGet, "/api/v1/balance", "key", nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestGetBalance_ServiceError(t *testing.T) {
	ls := &mockLedgerService{err: errors.New("db error")}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/balance", "sk", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestSubmitTransaction_MissingAPIKey(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-001", "type": "credit", "amount": 1000})
	rr := do(newTestAPI(nil, nil, nil, &mockResolver{}), http.MethodPost, "/api/v1/transactions", "", body)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSubmitTransaction_InvalidAmount(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-002", "type": "credit", "amount": -50})
	rr := do(newTestAPI(nil, nil, nil, newActiveResolver()), http.MethodPost, "/api/v1/transactions", "sk", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSubmitTransaction_InvalidType(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-003", "type": "transfer", "amount": 500})
	rr := do(newTestAPI(nil, nil, nil, newActiveResolver()), http.MethodPost, "/api/v1/transactions", "sk", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSubmitTransaction_Accepted(t *testing.T) {
	idem := &mockIdempotency{}
	pool := &mockPool{}
	ls := &mockLedgerService{txID: mockTxID}
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-ok-001", "type": "credit", "amount": 2000})
	rr := do(newTestAPI(ls, idem, pool, newActiveResolver()), http.MethodPost, "/api/v1/transactions", "sk", body)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d — body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["id"] != mockTxID {
		t.Errorf("expected id %q, got %v", mockTxID, resp["id"])
	}
	if resp["status"] != "pending" {
		t.Errorf("expected status pending, got %v", resp["status"])
	}
}

func TestSubmitTransaction_IdempotencyReplayed(t *testing.T) {
	existing := `{"id":"` + mockTxID + `","reference":"ref-dup","status":"pending"}`
	idem := &mockIdempotency{stored: map[string]string{"merchant_1:ref-dup": existing}, isReplayed: true}
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-dup", "type": "credit", "amount": 500})
	rr := do(newTestAPI(nil, idem, nil, newActiveResolver()), http.MethodPost, "/api/v1/transactions", "sk", body)

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

func TestSubmitTransaction_IdempotencyGetError(t *testing.T) {
	idem := &mockIdempotency{getErr: errors.New("redis down")}
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-005", "type": "credit", "amount": 100})
	rr := do(newTestAPI(nil, idem, nil, newActiveResolver()), http.MethodPost, "/api/v1/transactions", "sk", body)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestSubmitTransaction_CreateTxError(t *testing.T) {
	idem := &mockIdempotency{}
	ls := &mockLedgerService{err: errors.New("db constraint")}
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-006", "type": "credit", "amount": 100})
	rr := do(newTestAPI(ls, idem, nil, newActiveResolver()), http.MethodPost, "/api/v1/transactions", "sk", body)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestSubmitTransaction_QueueFull(t *testing.T) {
	idem := &mockIdempotency{}
	pool := &mockPool{err: errors.New("queue full")}
	ls := &mockLedgerService{txID: mockTxID}
	body, _ := json.Marshal(map[string]interface{}{"reference": "ref-007", "type": "credit", "amount": 100})
	rr := do(newTestAPI(ls, idem, pool, newActiveResolver()), http.MethodPost, "/api/v1/transactions", "sk", body)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
}

func TestGetTransactionByID_OK(t *testing.T) {
	tx := &domain.Transaction{
		ID: mockTxID, Reference: "ref-001", Type: "credit",
		Amount: 1500, Status: domain.TransactionStatusCompleted, CreatedAt: time.Now(),
	}
	ls := &mockLedgerService{transaction: tx}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/transactions/"+mockTxID, "sk", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}
}

func TestGetTransactionByID_NotFound(t *testing.T) {
	ls := &mockLedgerService{transaction: nil}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/transactions/nonexistent", "sk", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestGetTransactionByID_ServiceError(t *testing.T) {
	ls := &mockLedgerService{err: errors.New("db error")}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/transactions/"+mockTxID, "sk", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestListTransactions_OK(t *testing.T) {
	txs := []*domain.Transaction{
		{ID: "tx-1", Status: domain.TransactionStatusCompleted},
		{ID: "tx-2", Status: domain.TransactionStatusPending},
	}
	ls := &mockLedgerService{transactions: txs, total: 2}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/transactions?page=1&limit=10", "sk", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	pagination := resp["pagination"].(map[string]interface{})
	if pagination["total"] != float64(2) {
		t.Errorf("expected total 2, got %v", pagination["total"])
	}
}

func TestListTransactions_InvalidStatusFilter(t *testing.T) {
	ls := &mockLedgerService{}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/transactions?status=invalid", "sk", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListTransactions_EmptyResult(t *testing.T) {
	ls := &mockLedgerService{transactions: nil, total: 0}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/transactions", "sk", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected empty data array, got %d items", len(data))
	}
}

func TestListLedgerEntries_OK(t *testing.T) {
	entries := []*domain.LedgerEntry{
		{ID: "le-1", TransactionRef: "ref-001", ChangeAmount: 1500, PreviousBalance: 0, NewBalance: 1500, CreatedAt: time.Now()},
		{ID: "le-2", TransactionRef: "ref-002", ChangeAmount: -500, PreviousBalance: 1500, NewBalance: 1000, CreatedAt: time.Now()},
	}
	ls := &mockLedgerService{entries: entries, total: 2}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/ledger?page=1&limit=10", "sk", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	pagination := resp["pagination"].(map[string]interface{})
	if pagination["total"] != float64(2) {
		t.Errorf("expected total 2, got %v", pagination["total"])
	}
}

func TestListLedgerEntries_EmptyResult(t *testing.T) {
	ls := &mockLedgerService{entries: nil, total: 0}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/ledger", "sk", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected empty data array, got %d items", len(data))
	}
}

func TestListLedgerEntries_ServiceError(t *testing.T) {
	ls := &mockLedgerService{err: errors.New("db error")}
	rr := do(newTestAPI(ls, nil, nil, newActiveResolver()), http.MethodGet, "/api/v1/ledger", "sk", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

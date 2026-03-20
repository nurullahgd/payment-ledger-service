package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	tenantmw "github.com/nurullahgd/payment-ledger-service/internal/middleware"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

type LedgerService interface {
	GetCurrentBalance(ctx context.Context, merchantID string) (int64, string, error)
	CreatePendingTransaction(ctx context.Context, merchantID, reference, txType, description string, amount int64) (string, error)
}

type IdempotencyChecker interface {
	Get(ctx context.Context, merchantID, reference string) (string, bool, error)
	Set(ctx context.Context, merchantID, reference, payload string) error
}

type TaskSubmitter interface {
	Submit(task worker.TransactionTask) error
}

type API struct {
	pool          TaskSubmitter
	idempotency   IdempotencyChecker
	ledgerService LedgerService
	resolver      tenantmw.MerchantResolver
}

func NewAPI(pool TaskSubmitter, idempRepo IdempotencyChecker, ls LedgerService, resolver tenantmw.MerchantResolver) *API {
	return &API{
		pool:          pool,
		idempotency:   idempRepo,
		ledgerService: ls,
		resolver:      resolver,
	}
}

type ErrorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := ErrorResponse{}
	resp.Error.Code = code
	resp.Error.Message = message
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *API) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(tenantmw.TenantMiddleware(a.resolver))
		r.Post("/transactions", a.HandleSubmitTransaction)
		r.Get("/balance", a.GetBalance)
	})

	return r
}

type SubmitTransactionRequest struct {
	Reference   string                 `json:"reference"`
	Type        string                 `json:"type"`
	Amount      int64                  `json:"amount"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
}

func (a *API) HandleSubmitTransaction(w http.ResponseWriter, r *http.Request) {
	merchant := tenantmw.MerchantFromContext(r.Context())

	var req SubmitTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "Invalid JSON payload")
		return
	}

	if req.Reference == "" || (req.Type != "credit" && req.Type != "debit") || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid input parameters")
		return
	}

	cached, isReplayed, err := a.idempotency.Get(r.Context(), merchant.ID, req.Reference)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Idempotency check failed")
		return
	}

	if isReplayed {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Idempotency-Replayed", "true")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(cached))
		return
	}

	txID, err := a.ledgerService.CreatePendingTransaction(r.Context(), merchant.ID, req.Reference, req.Type, req.Description, req.Amount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not create transaction")
		return
	}

	respBytes, _ := json.Marshal(map[string]string{
		"id":        txID,
		"status":    "pending",
		"reference": req.Reference,
	})

	if err := a.idempotency.Set(r.Context(), merchant.ID, req.Reference, string(respBytes)); err != nil {
		log.Printf("failed to record idempotency key for ref %s: %v", req.Reference, err)
	}

	if err := a.pool.Submit(worker.TransactionTask{
		MerchantID:  merchant.ID,
		Reference:   req.Reference,
		Type:        req.Type,
		Amount:      req.Amount,
		Description: req.Description,
	}); err != nil {
		writeError(w, http.StatusTooManyRequests, "QUEUE_FULL", "System is under heavy load, please try again")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write(respBytes)
}

func (a *API) GetBalance(w http.ResponseWriter, r *http.Request) {
	merchant := tenantmw.MerchantFromContext(r.Context())

	balance, currency, err := a.ledgerService.GetCurrentBalance(r.Context(), merchant.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not fetch balance")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"merchant_id": merchant.ID,
		"balance":     balance,
		"currency":    currency,
	})
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/nurullahgd/payment-ledger-service/internal/domain"
	tenantmw "github.com/nurullahgd/payment-ledger-service/internal/middleware"
	"github.com/nurullahgd/payment-ledger-service/internal/ratelimit"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

type LedgerService interface {
	GetCurrentBalance(ctx context.Context, merchantID string) (int64, string, error)
	CreatePendingTransaction(ctx context.Context, merchantID, reference, txType, description string, amount int64) (string, error)
	GetTransactionByID(ctx context.Context, merchantID, txID string) (*domain.Transaction, error)
	ListTransactions(ctx context.Context, merchantID, statusFilter string, limit, offset int) ([]*domain.Transaction, int, error)
	ListLedgerEntries(ctx context.Context, merchantID string, limit, offset int) ([]*domain.LedgerEntry, int, error)
}

type IdempotencyChecker interface {
	Get(ctx context.Context, merchantID, reference string) (string, bool, error)
	Set(ctx context.Context, merchantID, reference, payload string) error
}

type TaskSubmitter interface {
	Submit(task worker.TransactionTask) error
}

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type API struct {
	pool          TaskSubmitter
	idempotency   IdempotencyChecker
	ledgerService LedgerService
	resolver      tenantmw.MerchantResolver
	rateLimiter   ratelimit.Limiter
	db            HealthChecker
	cache         HealthChecker
}

func NewAPI(pool TaskSubmitter, idempRepo IdempotencyChecker, ls LedgerService, resolver tenantmw.MerchantResolver, rateLimiter ratelimit.Limiter, db, cache HealthChecker) *API {
	return &API{
		pool:          pool,
		idempotency:   idempRepo,
		ledgerService: ls,
		resolver:      resolver,
		rateLimiter:   rateLimiter,
		db:            db,
		cache:         cache,
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

type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

type Pagination struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

func parsePagination(r *http.Request) (page, limit, offset int) {
	page = 1
	limit = 20

	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	offset = (page - 1) * limit
	return
}

func totalPages(total, limit int) int {
	if limit == 0 {
		return 0
	}
	pages := total / limit
	if total%limit != 0 {
		pages++
	}
	return pages
}

func (a *API) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)

	r.Get("/health", a.Health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(tenantmw.TenantMiddleware(a.resolver))
		r.Use(tenantmw.RateLimitMiddleware(a.rateLimiter))
		r.Post("/transactions", a.HandleSubmitTransaction)
		r.Get("/transactions", a.ListTransactions)
		r.Get("/transactions/{id}", a.GetTransactionByID)
		r.Get("/balance", a.GetBalance)
		r.Get("/ledger", a.ListLedgerEntries)
	})

	return r
}

func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{
		"db":    "ok",
		"cache": "ok",
	}
	httpStatus := http.StatusOK

	if a.db != nil {
		if err := a.db.Ping(r.Context()); err != nil {
			status["db"] = "unavailable"
			httpStatus = http.StatusServiceUnavailable
		}
	}

	if a.cache != nil {
		if err := a.cache.Ping(r.Context()); err != nil {
			status["cache"] = "unavailable"
			httpStatus = http.StatusServiceUnavailable
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(status)
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

func (a *API) GetTransactionByID(w http.ResponseWriter, r *http.Request) {
	merchant := tenantmw.MerchantFromContext(r.Context())
	txID := chi.URLParam(r, "id")

	tx, err := a.ledgerService.GetTransactionByID(r.Context(), merchant.ID, txID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not fetch transaction")
		return
	}
	if tx == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Transaction %s not found", txID))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tx)
}

func (a *API) ListTransactions(w http.ResponseWriter, r *http.Request) {
	merchant := tenantmw.MerchantFromContext(r.Context())
	statusFilter := r.URL.Query().Get("status")

	if statusFilter != "" &&
		statusFilter != string(domain.TransactionStatusPending) &&
		statusFilter != string(domain.TransactionStatusCompleted) &&
		statusFilter != string(domain.TransactionStatusFailed) {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", "status must be pending, completed, or failed")
		return
	}

	page, limit, offset := parsePagination(r)

	txs, total, err := a.ledgerService.ListTransactions(r.Context(), merchant.ID, statusFilter, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not list transactions")
		return
	}

	if txs == nil {
		txs = []*domain.Transaction{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PaginatedResponse{
		Data: txs,
		Pagination: Pagination{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages(total, limit),
		},
	})
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

func (a *API) ListLedgerEntries(w http.ResponseWriter, r *http.Request) {
	merchant := tenantmw.MerchantFromContext(r.Context())
	page, limit, offset := parsePagination(r)

	entries, total, err := a.ledgerService.ListLedgerEntries(r.Context(), merchant.ID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not list ledger entries")
		return
	}

	if entries == nil {
		entries = []*domain.LedgerEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PaginatedResponse{
		Data: entries,
		Pagination: Pagination{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages(total, limit),
		},
	})
}

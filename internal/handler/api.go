package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nurullahgd/payment-ledger-service/internal/repository"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

type API struct {
	pool        *worker.Pool
	idempotency *repository.IdempotencyRepository
}

func NewAPI(pool *worker.Pool, idempRepo *repository.IdempotencyRepository) *API {
	return &API{
		pool:        pool,
		idempotency: idempRepo,
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
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	r.Route("/api/v1", func(r chi.Router) {

		r.Post("/transactions", a.HandleSubmitTransaction)
	})

	return r
}

type SubmitTransactionRequest struct {
	Reference   string `json:"reference"`
	Type        string `json:"type"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
}

func (a *API) HandleSubmitTransaction(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing X-API-Key header")
		return
	}
	merchantID := apiKey

	var req SubmitTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "Invalid JSON payload")
		return
	}

	if req.Reference == "" || (req.Type != "credit" && req.Type != "debit") || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid input parameters")
		return
	}

	originalResponseMap := map[string]string{
		"status":    "pending",
		"reference": req.Reference,
	}
	originalResponseBytes, _ := json.Marshal(originalResponseMap)
	originalResponsePayload := string(originalResponseBytes)

	cachedPayload, isReplayed, err := a.idempotency.CheckOrRecord(r.Context(), merchantID, req.Reference, originalResponsePayload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not process idempotency check")
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if isReplayed {
		w.Header().Set("Idempotency-Replayed", "true")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(cachedPayload))
		return
	}

	taskErr := a.pool.Submit(worker.TransactionTask{
		MerchantID: merchantID,
		Reference:  req.Reference,
		Type:       req.Type,
		Amount:     req.Amount,
	})

	if taskErr != nil {
		writeError(w, http.StatusTooManyRequests, "QUEUE_FULL", "System is under heavy load, please try again")
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write(originalResponseBytes)
}

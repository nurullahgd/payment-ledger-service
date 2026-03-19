package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

type ErrorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
	} `json:"error"`
}

type API struct {
	pool *worker.Pool
}

func NewAPI(pool *worker.Pool) *API {
	return &API{pool: pool}
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
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Requests are scoped to a merchant via an X-API-Key header")
		return
	}
	merchantID := apiKey

	var req SubmitTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "Invalid JSON payload")
		return
	}

	if req.Reference == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Reference is required")
		return
	}
	if req.Type != "credit" && req.Type != "debit" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Type must be 'credit' or 'debit'")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Amount must be a positive integer")
		return
	}

	a.pool.Submit(worker.TransactionTask{
		MerchantID: merchantID,
		Reference:  req.Reference,
		Type:       req.Type,
		Amount:     req.Amount,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "pending",
		"reference": req.Reference,
	})
}

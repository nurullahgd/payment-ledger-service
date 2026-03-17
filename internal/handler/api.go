package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

type API struct {
	pool *worker.Pool
}

func NewAPI(pool *worker.Pool) *API {
	return &API{pool: pool}
}

func (a *API) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(a.TenantMiddleware)
		r.Post("/transactions", a.HandleSubmitTransaction)
	})

	return r
}

func (a *API) TenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			http.Error(w, "Missing X-API-Key", http.StatusUnauthorized)
			return
		}

		merchantID := apiKey

		r.Header.Set("X-Merchant-ID", merchantID)
		next.ServeHTTP(w, r)
	})
}

func (a *API) HandleSubmitTransaction(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Header.Get("X-Merchant-ID")

	var req struct {
		Reference   string `json:"reference"`
		Type        string `json:"type"`
		Amount      int64  `json:"amount"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	a.pool.Submit(worker.TransactionTask{
		MerchantID: merchantID,
		Reference:  req.Reference,
		Type:       req.Type,
		Amount:     req.Amount,
	})

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
}

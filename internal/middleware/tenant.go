package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nurullahgd/payment-ledger-service/internal/domain"
)

type contextKey string

const MerchantKey contextKey = "merchant"

type MerchantResolver interface {
	GetMerchantByAPIKey(ctx context.Context, apiKey string) (*domain.Merchant, error)
}

func TenantMiddleware(resolver MerchantResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				writeJSON(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing X-API-Key header")
				return
			}

			merchant, err := resolver.GetMerchantByAPIKey(r.Context(), apiKey)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not resolve merchant")
				return
			}
			if merchant == nil {
				writeJSON(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid API key")
				return
			}
			if !merchant.IsActive() {
				writeJSON(w, http.StatusForbidden, "MERCHANT_SUSPENDED", "Merchant account is suspended")
				return
			}

			ctx := context.WithValue(r.Context(), MerchantKey, merchant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func MerchantFromContext(ctx context.Context) *domain.Merchant {
	m, _ := ctx.Value(MerchantKey).(*domain.Merchant)
	return m
}

func writeJSON(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nurullahgd/payment-ledger-service/internal/ratelimit"
)

func RateLimitMiddleware(limiter ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			merchant := MerchantFromContext(r.Context())
			if merchant == nil {
				next.ServeHTTP(w, r)
				return
			}

			key := ratelimit.MerchantKey(merchant.ID)
			result, err := limiter.Allow(r.Context(), key)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Rate limiter unavailable")
				return
			}

			if !result.Allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", result.RetryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]string{
						"code":    "RATE_LIMIT_EXCEEDED",
						"message": fmt.Sprintf("Too many requests. Retry after %d seconds.", result.RetryAfter),
					},
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

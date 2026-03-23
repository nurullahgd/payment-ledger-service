package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nurullahgd/payment-ledger-service/internal/webhook"
)

func TestHTTPNotifier_Send_Success(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := webhook.NewHTTPNotifier()
	payload := webhook.Payload{
		TransactionID: "tx-1",
		Reference:     "ref-001",
		Status:        "completed",
		Amount:        1500,
		Timestamp:     time.Now(),
	}

	err := notifier.Send(context.Background(), server.URL, payload)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestHTTPNotifier_Send_RetryOnServerError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := webhook.NewHTTPNotifier()
	payload := webhook.Payload{
		TransactionID: "tx-2",
		Reference:     "ref-002",
		Status:        "completed",
		Amount:        500,
		Timestamp:     time.Now(),
	}

	err := notifier.Send(context.Background(), server.URL, payload)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", calls)
	}
}

func TestHTTPNotifier_Send_AllRetriesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notifier := webhook.NewHTTPNotifier()
	payload := webhook.Payload{
		TransactionID: "tx-3",
		Reference:     "ref-003",
		Status:        "failed",
		Amount:        200,
		Timestamp:     time.Now(),
	}

	err := notifier.Send(context.Background(), server.URL, payload)
	if err == nil {
		t.Error("expected error after all retries failed")
	}
}

func TestHTTPNotifier_Send_EmptyURL(t *testing.T) {
	notifier := webhook.NewHTTPNotifier()
	payload := webhook.Payload{Reference: "ref-004", Status: "completed"}

	err := notifier.Send(context.Background(), "", payload)
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

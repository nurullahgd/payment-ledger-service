package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type Payload struct {
	TransactionID string    `json:"transaction_id"`
	Reference     string    `json:"reference"`
	Status        string    `json:"status"`
	Amount        int64     `json:"amount"`
	Timestamp     time.Time `json:"timestamp"`
}

type Notifier interface {
	Send(ctx context.Context, webhookURL string, payload Payload) error
}

type HTTPNotifier struct {
	client     *http.Client
	maxRetries int
}

func NewHTTPNotifier() *HTTPNotifier {
	return &HTTPNotifier{
		client:     &http.Client{Timeout: 5 * time.Second},
		maxRetries: 3,
	}
}

func (n *HTTPNotifier) Send(ctx context.Context, webhookURL string, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= n.maxRetries; attempt++ {
		if err := n.attempt(ctx, webhookURL, body); err != nil {
			lastErr = err
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			log.Printf("[Webhook] attempt %d/%d failed for ref %s: %v — retrying in %s",
				attempt, n.maxRetries, payload.Reference, err, backoff)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}

		log.Printf("[Webhook] delivered for ref %s (attempt %d)", payload.Reference, attempt)
		return nil
	}

	log.Printf("[Webhook] all %d attempts failed for ref %s: %v", n.maxRetries, payload.Reference, lastErr)
	return lastErr
}

func (n *HTTPNotifier) attempt(ctx context.Context, webhookURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook endpoint returned %d", resp.StatusCode)
	}

	return nil
}

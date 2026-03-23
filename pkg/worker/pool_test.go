package worker_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nurullahgd/payment-ledger-service/internal/webhook"
	"github.com/nurullahgd/payment-ledger-service/pkg/worker"
)

type mockProcessor struct {
	mu    sync.Mutex
	calls []string
}

func (m *mockProcessor) ProcessTransaction(_ context.Context, _ string, ref string, _ string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, ref)
	return nil
}

func (m *mockProcessor) GetTransactionID(_ context.Context, _, ref string) (string, error) {
	return "tx-" + ref, nil
}

type mockNotifier struct {
	count atomic.Int32
}

func (n *mockNotifier) Send(_ context.Context, _ string, _ webhook.Payload) error {
	n.count.Add(1)
	return nil
}

func TestPool_ProcessesTask(t *testing.T) {
	proc := &mockProcessor{}
	notifier := &mockNotifier{}

	pool := worker.NewPool(1, 10, proc, notifier)
	pool.Start(context.Background())

	if err := pool.Submit(worker.TransactionTask{
		MerchantID: "merchant_1",
		Reference:  "ref-001",
		Type:       "credit",
		Amount:     1000,
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	pool.Stop()

	proc.mu.Lock()
	defer proc.mu.Unlock()
	if len(proc.calls) != 1 || proc.calls[0] != "ref-001" {
		t.Errorf("expected ref-001 to be processed, got %v", proc.calls)
	}
}

func TestPool_WebhookFiredOnTerminal(t *testing.T) {
	proc := &mockProcessor{}
	notifier := &mockNotifier{}

	pool := worker.NewPool(1, 10, proc, notifier)
	pool.Start(context.Background())

	if err := pool.Submit(worker.TransactionTask{
		MerchantID: "merchant_1",
		Reference:  "ref-wh",
		Type:       "credit",
		Amount:     500,
		WebhookURL: "http://example.com/webhook",
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	pool.Stop()

	if notifier.count.Load() != 1 {
		t.Errorf("expected 1 webhook call, got %d", notifier.count.Load())
	}
}

func TestPool_WebhookNotFiredWhenURLEmpty(t *testing.T) {
	proc := &mockProcessor{}
	notifier := &mockNotifier{}

	pool := worker.NewPool(1, 10, proc, notifier)
	pool.Start(context.Background())

	if err := pool.Submit(worker.TransactionTask{
		MerchantID: "merchant_1",
		Reference:  "ref-nowh",
		Type:       "credit",
		Amount:     100,
		WebhookURL: "",
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	pool.Stop()

	if notifier.count.Load() != 0 {
		t.Errorf("expected 0 webhook calls, got %d", notifier.count.Load())
	}
}

func TestPool_QueueFullReturnsError(t *testing.T) {
	proc := &mockProcessor{}
	pool := worker.NewPool(0, 1, proc, nil)

	if err := pool.Submit(worker.TransactionTask{Reference: "ref-1"}); err != nil {
		t.Fatalf("first submit should succeed: %v", err)
	}
	if err := pool.Submit(worker.TransactionTask{Reference: "ref-2"}); err == nil {
		t.Error("second submit should fail with queue full")
	}
}

func TestPool_GracefulShutdown(t *testing.T) {
	proc := &mockProcessor{}
	pool := worker.NewPool(2, 10, proc, nil)
	pool.Start(context.Background())

	for i := 0; i < 5; i++ {
		_ = pool.Submit(worker.TransactionTask{
			MerchantID: "m",
			Reference:  "ref",
			Type:       "credit",
			Amount:     10,
		})
	}

	done := make(chan struct{})
	go func() {
		pool.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("pool.Stop() did not return within 5 seconds")
	}
}

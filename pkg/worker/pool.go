package worker

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/nurullahgd/payment-ledger-service/internal/webhook"
)

type TransactionTask struct {
	MerchantID  string
	Reference   string
	Type        string
	Amount      int64
	Description string
	WebhookURL  string
}

type LedgerProcessor interface {
	ProcessTransaction(ctx context.Context, merchantID string, txRef string, txType string, amount int64) error
	GetTransactionID(ctx context.Context, merchantID, reference string) (string, error)
}

type Pool struct {
	tasks       chan TransactionTask
	wg          sync.WaitGroup
	ledgerRepo  LedgerProcessor
	notifier    webhook.Notifier
	workerCount int
}

func NewPool(workerCount int, queueSize int, repo LedgerProcessor, notifier webhook.Notifier) *Pool {
	return &Pool{
		tasks:       make(chan TransactionTask, queueSize),
		ledgerRepo:  repo,
		notifier:    notifier,
		workerCount: workerCount,
	}
}

func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	log.Printf("Started %d background workers", p.workerCount)
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for task := range p.tasks {
		taskCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		finalStatus := "completed"
		err := p.ledgerRepo.ProcessTransaction(taskCtx, task.MerchantID, task.Reference, task.Type, task.Amount)
		if err != nil {
			finalStatus = "failed"
			log.Printf("[Worker %d] Error processing task %s for merchant %s: %v", id, task.Reference, task.MerchantID, err)
		} else {
			log.Printf("[Worker %d] Successfully processed task %s", id, task.Reference)
		}

		if task.WebhookURL != "" && p.notifier != nil {
			txID, lookupErr := p.ledgerRepo.GetTransactionID(taskCtx, task.MerchantID, task.Reference)
			if lookupErr != nil {
				log.Printf("[Worker %d] Could not resolve txID for webhook %s: %v", id, task.Reference, lookupErr)
			} else {
				payload := webhook.Payload{
					TransactionID: txID,
					Reference:     task.Reference,
					Status:        finalStatus,
					Amount:        task.Amount,
					Timestamp:     time.Now().UTC(),
				}
				webhookCtx, webhookCancel := context.WithTimeout(context.Background(), 30*time.Second)
				if webhookErr := p.notifier.Send(webhookCtx, task.WebhookURL, payload); webhookErr != nil {
					log.Printf("[Worker %d] Webhook delivery failed for ref %s: %v", id, task.Reference, webhookErr)
				}
				webhookCancel()
			}
		}

		cancel()
	}
}

func (p *Pool) Submit(task TransactionTask) error {
	select {
	case p.tasks <- task:
		return nil
	default:
		return errors.New("worker pool queue is full")
	}
}

func (p *Pool) Stop() {
	log.Println("Graceful shutdown initiated. Stopping worker pool...")

	close(p.tasks)
	p.wg.Wait()

	log.Println("Worker pool stopped gracefully. All pending transactions finished.")
}

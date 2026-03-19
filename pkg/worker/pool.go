package worker

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

type TransactionTask struct {
	MerchantID string
	Reference  string
	Type       string
	Amount     int64
}

type LedgerProcessor interface {
	ProcessTransaction(ctx context.Context, merchantID string, txRef string, txType string, amount int64) error
}

type Pool struct {
	tasks       chan TransactionTask
	wg          sync.WaitGroup
	ledgerRepo  LedgerProcessor
	workerCount int
}

func NewPool(workerCount int, queueSize int, repo LedgerProcessor) *Pool {
	return &Pool{
		tasks:       make(chan TransactionTask, queueSize),
		ledgerRepo:  repo,
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

		err := p.ledgerRepo.ProcessTransaction(taskCtx, task.MerchantID, task.Reference, task.Type, task.Amount)
		if err != nil {
			log.Printf("[Worker %d] Error processing task %s for merchant %s: %v", id, task.Reference, task.MerchantID, err)
		} else {
			log.Printf("[Worker %d] Successfully processed task %s", id, task.Reference)
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

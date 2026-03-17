package worker

import (
	"context"
	"log"
	"sync"

	"github.com/nurullahgd/payment-ledger-service/internal/repository"
)

type TransactionTask struct {
	MerchantID string
	Reference  string
	Type       string
	Amount     int64
}

type Pool struct {
	tasks       chan TransactionTask //buffered channel
	wg          sync.WaitGroup
	ledgerRepo  repository.LedgerRepository
	workerCount int
}

func NewPool(workerCount int, repo *repository.LedgerRepository) *Pool {
	return &Pool{
		tasks:       make(chan TransactionTask),
		wg:          sync.WaitGroup{},
		ledgerRepo:  *repo,
		workerCount: workerCount,
	}
}
func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	for task := range p.tasks {
		log.Printf("Worker %d processing task: %+v", id, task)

		err := p.ledgerRepo.ProcessTransaction(ctx, task.MerchantID, task.Reference, task.Type, task.Amount)
		if err != nil {
			log.Printf("Worker %d error processing task: %v", id, err)
		}
	}
}

func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
	log.Printf("Started %d background workers", p.workerCount)
}

func (p *Pool) Submit(task TransactionTask) {
	p.tasks <- task
}

func (p *Pool) Stop() {
	
	log.Println("Graceful shutdown initiated. Stopping worker pool...")
	close(p.tasks)

	p.wg.Wait()
	log.Println("Worker pool stopped gracefully. All pending transactions finished.")
}

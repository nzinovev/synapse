package queue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nzinovev/synapse/internal/engine"
)

type WorkerPool struct {
	store        QueueStore
	engine       *engine.PipelineEngine
	workers      int
	stopCh       chan struct{}
	wg           sync.WaitGroup
	recoveryDone bool
	recoveryMu   sync.Mutex
}

func NewWorkerPool(store QueueStore, eng *engine.PipelineEngine, workers int) *WorkerPool {
	if workers < 1 {
		workers = 1
	}
	return &WorkerPool{
		store:   store,
		engine:  eng,
		workers: workers,
		stopCh:  make(chan struct{}),
	}
}

func (p *WorkerPool) Start() {
	p.recoveryMu.Lock()
	if p.recoveryDone {
		p.recoveryMu.Unlock()
		return
	}
	p.recoveryDone = true
	p.recoveryMu.Unlock()

	if err := p.store.ResetOrphanedItems(context.Background()); err != nil {
		log.Printf("queue: reset orphaned items: %v", err)
	}
	if err := p.store.ReenqueueRunningTasks(context.Background()); err != nil {
		log.Printf("queue: reenqueue running tasks: %v", err)
	}

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.runWorker(i)
	}
	log.Printf("queue: started %d workers", p.workers)
}

func (p *WorkerPool) Stop() {
	close(p.stopCh)
	p.wg.Wait()
	log.Printf("queue: all workers stopped")
}

func (p *WorkerPool) runWorker(id int) {
	defer p.wg.Done()
	workerID := fmt.Sprintf("worker-%d", id)

	for {
		select {
		case <-p.stopCh:
			return
		default:
		}

		item, err := p.store.Dequeue(context.Background(), workerID)
		if err != nil {
			log.Printf("queue: worker %s dequeue error: %v", workerID, err)
			time.Sleep(2 * time.Second)
			continue
		}
		if item == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		log.Printf("queue: worker %s processing task=%s action=%s", workerID, item.TaskID, item.Action)
		p.processItem(item)
		log.Printf("queue: worker %s done task=%s action=%s", workerID, item.TaskID, item.Action)

		if err := p.store.CompleteItem(context.Background(), item.ID); err != nil {
			log.Printf("queue: complete item %d: %v", item.ID, err)
		}
	}
}

func (p *WorkerPool) processItem(item *QueueItem) {
	ctx := context.Background()
	var err error

	switch item.Action {
	case "approve":
		_, err = p.engine.Approve(ctx, item.TaskID)
	case "reject":
		feedback := ""
		if item.Feedback != nil {
			feedback = *item.Feedback
		}
		_, err = p.engine.Reject(ctx, item.TaskID, feedback)
	case "answer":
		feedback := ""
		if item.Feedback != nil {
			feedback = *item.Feedback
		}
		_, err = p.engine.Answer(ctx, item.TaskID, feedback)
	case "retry":
		_, err = p.engine.Retry(ctx, item.TaskID)
	case "run":
		_, err = p.engine.RunUntilGate(ctx, item.TaskID)
	default:
		log.Printf("queue: unknown action %q for task %s", item.Action, item.TaskID)
		return
	}

	if err != nil {
		log.Printf("queue: error processing task=%s action=%s: %v", item.TaskID, item.Action, err)
	}
}

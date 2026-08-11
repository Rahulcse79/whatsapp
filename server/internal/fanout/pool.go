package fanout

import (
	"context"
	"log/slog"
)

// Pool runs a bounded set of fan-out workers (N=8 default, core-api-lld §3) over
// a buffered job queue. Submit is non-blocking: a full queue is backpressure
// (the caller leaves the message unacked so NATS redelivers), never unbounded
// pod memory. QueueDepth is the SLI that drives the HPA.
type Pool struct {
	worker  *Worker
	jobs    chan FanoutJob
	workers int
	log     *slog.Logger
}

func NewPool(worker *Worker, workers, queueDepth int, log *slog.Logger) *Pool {
	if workers <= 0 {
		workers = 8
	}
	if queueDepth <= 0 {
		queueDepth = 1024
	}
	return &Pool{worker: worker, jobs: make(chan FanoutJob, queueDepth), workers: workers, log: log}
}

// Start launches the worker goroutines; they stop when ctx is cancelled.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		go p.run(ctx)
	}
}

// Submit enqueues a job without blocking. Returns false when the queue is full.
func (p *Pool) Submit(job FanoutJob) bool {
	select {
	case p.jobs <- job:
		return true
	default:
		return false
	}
}

// QueueDepth is the current backlog (queue-depth SLI / HPA trigger).
func (p *Pool) QueueDepth() int { return len(p.jobs) }

func (p *Pool) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-p.jobs:
			if _, err := p.worker.Fanout(ctx, job); err != nil {
				p.log.Error("fan-out failed", "msg_uuid", job.MsgUUID, "group", job.GroupID, "err", err)
			}
		}
	}
}

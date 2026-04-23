package queue

import (
	"context"
	"time"

	"github.com/blesta/wa-reminder/internal/service"
)

type Processor struct {
	worker *service.WorkerService
}

func NewProcessor(worker *service.WorkerService) *Processor {
	return &Processor{worker: worker}
}

func (p *Processor) Start(ctx context.Context, pollInterval time.Duration) {
	p.worker.Start(ctx, pollInterval)
}

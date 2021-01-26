package middleware

import (
	"context"
	"sync/atomic"
)

// Counter count how many jobs have started and finished.
type Counter struct {
	started  uint64
	finished uint64
}

func (c *Counter) Middleware() func(func(context.Context)) func(context.Context) {
	return func(job func(context.Context)) func(context.Context) {
		return func(ctx context.Context) {
			atomic.AddUint64(&c.started, 1)
			job(ctx)
			atomic.AddUint64(&c.finished, 1)
		}
	}
}

func (c *Counter) Started() uint64 {
	return atomic.LoadUint64(&c.started)
}

func (c *Counter) Finished() uint64 {
	return atomic.LoadUint64(&c.finished)
}

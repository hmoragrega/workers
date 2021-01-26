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

// Middleware returns the job middleware that can be used
// when creating a new pool.
func (c *Counter) Middleware() func(func(context.Context)) func(context.Context) {
	return func(job func(context.Context)) func(context.Context) {
		return func(ctx context.Context) {
			atomic.AddUint64(&c.started, 1)
			job(ctx)
			atomic.AddUint64(&c.finished, 1)
		}
	}
}

// Started returns the number of jobs that have been started.
func (c *Counter) Started() uint64 {
	return atomic.LoadUint64(&c.started)
}

// Finished returns the number of jobs that have been finished.
func (c *Counter) Finished() uint64 {
	return atomic.LoadUint64(&c.finished)
}

// Running returns the number of jobs that are running now.
func (c *Counter) Running() uint64 {
	return c.Started() - c.Finished()
}

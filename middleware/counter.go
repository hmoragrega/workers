package middleware

import (
	"context"
	"sync/atomic"

	"github.com/hmoragrega/workers"
)

// Counter count how many jobs have started and finished.
// Keep in mind that it's not protected against overflows.
type Counter struct {
	started  uint64
	finished uint64
	failed   uint64
}

// Wrap wraps the job adding counters.
func (c *Counter) Wrap(next workers.Job) workers.Job {
	return workers.JobFunc(func(ctx context.Context) error {
		atomic.AddUint64(&c.started, 1)
		err := next.Do(ctx)
		atomic.AddUint64(&c.finished, 1)
		if err != nil {
			atomic.AddUint64(&c.failed, 1)
		}
		return err
	})
}

// Started returns the number of jobs that have been started.
func (c *Counter) Started() uint64 {
	return atomic.LoadUint64(&c.started)
}

// Finished returns the number of jobs that have been finished.
func (c *Counter) Finished() uint64 {
	return atomic.LoadUint64(&c.finished)
}

// Running returns the number of failed jobs.
func (c *Counter) Failed() uint64 {
	return atomic.LoadUint64(&c.failed)
}

// Running returns the number of jobs that are running now.
func (c *Counter) Running() uint64 {
	return c.Started() - c.Finished()
}

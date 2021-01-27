package middleware

import (
	"context"
	"time"

	"github.com/hmoragrega/workers"
)

// Wait will add a pause between calls to the next job.
// The pause affects only jobs between the same worker.
func Wait(wait time.Duration) workers.MiddlewareFunc {
	return func(job workers.Job) workers.Job {
		var (
			ticker *time.Ticker
			tick   <-chan time.Time
		)
		if wait > 0 {
			ticker = time.NewTicker(wait)
			tick = ticker.C
		} else {
			ch := make(chan time.Time)
			close(ch)
			tick = ch
		}
		return workers.JobFunc(func(ctx context.Context) {
			select {
			case <-ctx.Done():
				if ticker != nil {
					ticker.Stop()
				}
				return
			case <-tick:
				job.Do(ctx)
			}
		})
	}
}

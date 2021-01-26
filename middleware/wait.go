package middleware

import (
	"context"
	"time"
)

// Wait will add a pause between calls to the next job.
// The pause affects only jobs between the same worker.
func Wait(wait time.Duration) func(func(context.Context)) func(context.Context) {
	return func(job func(context.Context)) func(context.Context) {
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
		return func(ctx context.Context) {
			select {
			case <-ctx.Done():
				if ticker != nil {
					ticker.Stop()
				}
				return
			case <-tick:
				job(ctx)
			}
		}
	}
}

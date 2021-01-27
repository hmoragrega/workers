package middleware

import (
	"context"
	"sync"
	"time"
)

// Elapsed is a job middleware that extends the simple counter
// and calculates the total time, average time and the last
// time spent doing the job.
type Elapsed struct {
	Counter
	total   time.Duration
	average time.Duration
	last    time.Duration
	mx      sync.RWMutex

	since func(time.Time) time.Duration
}

// Wrap wraps the inner job to obtain job timing metrics.
func (e *Elapsed) Wrap(next func(context.Context)) func(context.Context) {
	e.mx.Lock()
	if e.since == nil {
		e.since = time.Since
	}
	e.mx.Unlock()

	// wrap incoming job with the counter.
	next = e.Counter.Wrap(next)
	return func(ctx context.Context) {
		start := time.Now()
		next(ctx)
		elapsed := e.since(start)
		count := e.Counter.Finished()

		e.mx.Lock()
		e.last = elapsed
		e.total += e.last
		e.average = e.total / time.Duration(count)
		e.mx.Unlock()
	}
}

// Total returns the total time spent executing
// all the job across all the workers.
func (e *Elapsed) Total() time.Duration {
	e.mx.RLock()
	defer e.mx.RUnlock()

	return e.total
}

// Last returns the time spent executing the last job.
func (e *Elapsed) Last() time.Duration {
	e.mx.RLock()
	defer e.mx.RUnlock()

	return e.last
}

// Average returns the average time that takes to
// run the job.
func (e *Elapsed) Average() time.Duration {
	e.mx.RLock()
	defer e.mx.RUnlock()

	return e.average
}

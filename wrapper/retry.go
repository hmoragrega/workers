package wrapper

import (
	"context"
	"errors"

	"github.com/hmoragrega/workers"
)

var ErrNotRetryable = errors.New("not retryable")

// WithRetry is a job wrapper that allows to retry a job
// if it returns an error.
//
// If you consider than the error is not retryable you
// can either return nil or the custom "ErrNotRetryable".
func WithRetry(job func(context.Context) error, retries uint) workers.Job {
	attemptsRemaining := retries + 1
	return workers.JobFunc(func(ctx context.Context) {
		for attemptsRemaining > 0 {
			attemptsRemaining--

			err := job(ctx)
			if err != nil && !errors.Is(err, ErrNotRetryable) {
				continue
			}
			return
		}
	})
}

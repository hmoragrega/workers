package wrapper

import (
	"context"
	"errors"
)

var ErrNotRetryable = errors.New("not retryable")

// WithRetry is a job wrapper that allows to retry a job
// if it returns an error.
//
// If you consider than the error is not retryable you
// can either return nil or the custom "ErrNotRetryable".
func WithRetry(job func(context.Context) error, retries uint) func(context.Context) {
	attemptsRemaining := retries + 1
	return func(ctx context.Context) {
		for attemptsRemaining > 0 {
			attemptsRemaining--

			err := job(ctx)
			if err != nil && !errors.Is(err, ErrNotRetryable) {
				continue
			}
			return
		}
	}
}

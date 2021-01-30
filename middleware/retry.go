package middleware

import (
	"context"
	"errors"

	"github.com/hmoragrega/workers"
)

// ErrNotRetryable should be returned by the job
// to avoid retrying it.
var ErrNotRetryable = errors.New("not retryable")

// Retry is a job middleware that allows to retry a job
// if it returns an error.
//
// If you consider that the error is not retryable you
// can either return nil or the custom "ErrNotRetryable".
func Retry(retries uint) workers.Middleware {
	return workers.MiddlewareFunc(func(job workers.Job) workers.Job {
		return workers.JobFunc(func(ctx context.Context) (err error) {
			attempts := retries + 1
			for attempts > 0 {
				err = job.Do(ctx)

				if err == nil {
					return nil
				}
				if errors.Is(err, ErrNotRetryable) {
					return err
				}

				attempts--
				continue
			}
			return err
		})
	})
}

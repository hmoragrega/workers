package wrapper

import (
	"context"

	"github.com/hmoragrega/workers"
)

// WithError is a job wrapper that allows to use jobs
// that return errors.
func WithError(job func(context.Context) error, result func(error)) workers.Job {
	return workers.JobFunc(func(ctx context.Context) {
		result(job(ctx))
	})
}

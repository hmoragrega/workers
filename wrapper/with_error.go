package wrapper

import (
	"context"
)

// WithError is a job wrapper that allows to use jobs
// that return errors.
func WithError(job func(context.Context) error, result func(error)) func(context.Context) {
	return func(ctx context.Context) {
		result(job(ctx))
	}
}

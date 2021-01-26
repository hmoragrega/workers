package wrapper

import (
	"context"
)

// WithErrorResult is a job wrapper that allows to receive
// the error result of the job.
//
// For example it can be used to build logging or monitoring
// pipelines on the job.
func WithErrorResult(job func(context.Context) error, result func(error)) func(context.Context) {
	return func(ctx context.Context) {
		result(job(ctx))
	}
}

package middleware

import (
	"context"

	"github.com/hmoragrega/workers"
)

// NoError wraps a job returning always nil
func NoError(job func(ctx context.Context)) workers.Job {
	return workers.JobFunc(func(ctx context.Context) error {
		job(ctx)
		return nil
	})
}

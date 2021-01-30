package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/hmoragrega/workers"
)

func TestWithRetryWrapper(t *testing.T) {
	var dummyErr = errors.New("dummy err")
	tests := []struct {
		name    string
		retries uint
		results []error
		calls   int
		want    error
	}{
		{
			name:    "retries exhausted",
			retries: 3,
			results: []error{dummyErr, dummyErr, dummyErr, dummyErr},
			calls:   4,
			want:    dummyErr,
		},
		{
			name:    "first try success",
			retries: 3,
			results: []error{nil},
			calls:   1,
			want:    nil,
		},
		{
			name:    "non-retryable error",
			retries: 3,
			results: []error{dummyErr, ErrNotRetryable},
			calls:   2,
			want:    ErrNotRetryable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var count int
			job := workers.JobFunc(func(ctx context.Context) error {
				count++
				return tc.results[count-1]
			})

			got := Retry(tc.retries).Wrap(job).Do(context.Background())

			if !errors.Is(got, tc.want) {
				t.Fatalf("unexpected job result; got %d, want %d", got, tc.want)
			}

			if count != tc.calls {
				t.Fatalf("unexpected number of job executions; got %d, want %d", count, tc.calls)
			}
		})
	}
}

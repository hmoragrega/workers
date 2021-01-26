package wrapper

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestWithRetryWrapper(t *testing.T) {
	var dummyErr = errors.New("dummy err")
	tests := []struct {
		name    string
		retries uint
		results []error
		want    uint32
	}{
		{
			name:    "retries exhausted",
			retries: 3,
			results: []error{dummyErr, dummyErr, dummyErr, dummyErr},
			want:    4,
		},
		{
			name:    "first try success",
			retries: 3,
			results: []error{nil},
			want:    1,
		},
		{
			name:    "non-retryable error",
			retries: 3,
			results: []error{dummyErr, ErrNotRetryable},
			want:    2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var count uint32
			job := func(ctx context.Context) error {
				i := atomic.AddUint32(&count, 1)
				return tc.results[i-1]
			}

			wrapped := WithRetry(job, tc.retries)
			wrapped(context.Background())

			if got := atomic.LoadUint32(&count); got != tc.want {
				t.Fatalf("unexpected number of job executions; got %d, want %d", got, tc.want)
			}
		})
	}
}

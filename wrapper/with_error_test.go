package wrapper

import (
	"context"
	"errors"
	"testing"
)

func TestWithErrorWrapper(t *testing.T) {
	var dummyErr = errors.New("dummy err")
	tests := []struct {
		name    string
		retries uint
		result  error
		want    error
	}{
		{
			name:   "no error",
			result: nil,
			want:   nil,
		},
		{
			name:   "job error",
			result: dummyErr,
			want:   dummyErr,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got error
			job := func(ctx context.Context) error {
				return tc.want
			}
			wrapped := WithError(job, func(err error) {
				got = err
			})

			wrapped.Do(context.Background())

			if got != tc.want {
				t.Fatalf("unexpected job result; got %d, want %d", got, tc.want)
			}
		})
	}
}

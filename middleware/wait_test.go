package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/hmoragrega/workers"
)

func TestWaitMiddleware_Wait(t *testing.T) {
	tests := []struct {
		name      string
		wait      time.Duration
		threshold time.Duration
	}{
		{
			name:      "waiting 100ms",
			wait:      100 * time.Millisecond,
			threshold: 20 * time.Millisecond,
		},
		{
			name:      "no wait",
			wait:      0,
			threshold: 20 * time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stop := make(chan time.Time)
			job := func(ctx context.Context) {
				stop <- time.Now()
				<-ctx.Done()
			}

			p := workers.Must(workers.New(job, workers.JobMiddlewareFunc(Wait(tc.wait))))

			poolStarted := time.Now()
			if err := p.Start(); err != nil {
				t.Fatal("cannot start pool", err)
			}
			jobStarted := <-stop

			if err := p.Close(context.Background()); err != nil {
				t.Fatal("cannot stop pool", err)
			}

			pausedFor := jobStarted.Sub(poolStarted)
			if got, want := pausedFor-tc.wait, tc.threshold; got > want {
				t.Fatalf("uneexpected wait time; got %s, want %s", got, want)
			}
		})
	}
}

func TestWaitMiddleware_Cancelled(t *testing.T) {
	executed := make(chan struct{})
	job := func(ctx context.Context) {
		close(executed)
	}

	p := workers.Must(workers.New(job, workers.JobMiddlewareFunc(Wait(time.Second))))
	if err := p.Start(); err != nil {
		t.Fatal("cannot start pool", err)
	}

	select {
	case <-time.NewTimer(100 * time.Millisecond).C:
	case <-executed:
		t.Fatal("job executed before the wait time")
	}

	if err := p.Close(context.Background()); err != nil {
		t.Fatal("cannot stop pool", err)
	}

	select {
	case <-executed:
		t.Fatal("job has been executed after stopping the pool")
	default:
	}
}

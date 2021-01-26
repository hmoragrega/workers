package middleware

import (
	"context"
	"testing"

	"github.com/hmoragrega/workers"
)

func TestCounterMiddleware(t *testing.T) {
	var (
		counter Counter
		stopAt  uint64 = 10
		stop           = make(chan struct{})
	)

	job := func(ctx context.Context) {
		if counter.Started() == stopAt {
			// trigger the stop of the pool an wait for
			// pool context cancellation to prevent new jobs
			close(stop)
			<-ctx.Done()
		}
	}

	p := workers.Must(workers.New(job, &counter))
	if err := p.Start(); err != nil {
		t.Fatal("cannot start pool", err)
	}

	<-stop

	if got, want := counter.Running(), uint64(1); got != want {
		t.Fatalf("unexpected number of running; got %d, want %d", got, want)
	}

	if err := p.Close(context.Background()); err != nil {
		t.Fatal("cannot stop pool", err)
	}

	if got, want := counter.Started(), stopAt; got != want {
		t.Fatalf("unexpected number of started jobs; got %d, want %d", got, want)
	}
	if got, want := counter.Finished(), stopAt; got != want {
		t.Fatalf("unexpected number of started jobs; got %d, want %d", got, want)
	}
}

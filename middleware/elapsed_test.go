package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/hmoragrega/workers"
)

func TestElapsedMiddleware(t *testing.T) {
	var (
		elapsed Elapsed
		stopAt  uint64 = 10
		stop           = make(chan struct{})
		total          = 55 * time.Second // 10 + 9 + 8 ... 1
		average        = total / time.Duration(stopAt)
		last           = 10 * time.Second
	)

	job := workers.JobFunc(func(ctx context.Context) error {
		// make every job execution 1 second longer than the previous one.
		elapsed.since = func(time.Time) time.Duration {
			return time.Second * time.Duration(elapsed.Started())
		}
		if elapsed.Started() == stopAt {
			close(stop)
			<-ctx.Done()
		}
		return nil
	})

	p := workers.Must(workers.New(&elapsed))
	if err := p.Start(job); err != nil {
		t.Fatal("cannot start pool", err)
	}

	<-stop
	if err := p.Close(context.Background()); err != nil {
		t.Fatal("cannot stop pool", err)
	}

	if got, want := elapsed.Started(), stopAt; got != want {
		t.Fatalf("unexpected number of started jobs; got %d, want %d", got, want)
	}
	if got, want := elapsed.Finished(), stopAt; got != want {
		t.Fatalf("unexpected number of started jobs; got %d, want %d", got, want)
	}

	if got, want := elapsed.Total(), total; got != want {
		t.Fatalf("unexpected total elapsed time; got %d, want %d", got, want)
	}
	if got, want := elapsed.Average(), average; got != want {
		t.Fatalf("unexpected average time; got %d, want %d", got, want)
	}
	if got, want := elapsed.Last(), last; got != want {
		t.Fatalf("unexpected average time; got %d, want %d", got, want)
	}
}

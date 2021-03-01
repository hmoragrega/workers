package workers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	dummyJob = JobFunc(func(_ context.Context) error {
		return nil
	})
	slowJob = JobFunc(func(_ context.Context) error {
		<-time.NewTimer(150 * time.Millisecond).C
		return nil
	})
)

func TestPool_New(t *testing.T) {
	testCases := []struct {
		name   string
		config Config
		want   error
	}{
		{
			name: "invalid min",
			config: Config{
				Min: -1,
			},
			want: ErrInvalidMin,
		},
		{
			name: "invalid max",
			config: Config{
				Min: 5,
				Max: 3,
			},
			want: ErrInvalidMax,
		},
		{
			name: "invalid initial",
			config: Config{
				Min:     5,
				Initial: 3,
			},
			want: ErrInvalidInitial,
		},
		{
			name: "ok",
			config: Config{
				Initial: 7,
				Min:     5,
				Max:     10,
			},
			want: nil,
		},
		{
			name: "ok with defaults",
			want: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := NewWithConfig(tc.config)
			if !errors.Is(got, tc.want) {
				t.Fatalf("got error %+v, want: %+v", got, tc.want)
			}
		})
	}
}

func TestPool_MustPanic(t *testing.T) {
	defer func() {
		err := recover()
		if err == nil {
			t.Fatal("expected panic")
		}
		if !errors.Is(err.(error), ErrInvalidMax) {
			t.Fatalf("unexpected panic error; got %+v, want %v", err, ErrInvalidMax)
		}
	}()

	Must(NewWithConfig(Config{Max: -1}))
}

func TestPool_Start(t *testing.T) {
	testCases := []struct {
		name   string
		config Config
		want   int
	}{
		{
			name: "initial value",
			config: Config{
				Initial: 7,
			},
			want: 7,
		},
		{
			name: "default ok",
			want: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := Must(NewWithConfig(tc.config))
			t.Cleanup(func() {
				if err := p.CloseWithTimeout(time.Second); err != nil {
					t.Fatal("cannot stop pool", err)
				}
			})

			if err := p.Start(dummyJob); err != nil {
				t.Fatalf("unexpected error starting poole. got %+v", err)
			}
			if got := p.Current(); got != tc.want {
				t.Fatalf("unexpected initial workers. got %d, want: %d", got, tc.want)
			}
		})
	}
}

func TestPool_StartErrors(t *testing.T) {
	t.Run("pool closed", func(t *testing.T) {
		var p Pool
		if err := p.CloseWithTimeout(time.Second); err != nil {
			t.Fatalf("unexpected error closing an uninitialized pool; got %+v", err)
		}

		if got, want := p.Start(dummyJob), ErrPoolClosed; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("pool already started", func(t *testing.T) {
		var p Pool
		if err := p.Start(dummyJob); err != nil {
			t.Fatalf("unexpected error starting for the first time; got %+v", err)
		}

		if got, want := p.Start(dummyJob), ErrPoolStarted; got != want {
			t.Fatalf("unexpected error starting pool for the second time; got %+v, want %+v", got, want)
		}
	})
}

func TestPool_StartWithBuilder(t *testing.T) {
	numWorkers := 3

	p := Must(NewWithConfig(Config{
		Initial: numWorkers,
	}))

	var builds int32
	var buildsWG sync.WaitGroup
	buildsWG.Add(numWorkers)

	counters := make([]int, numWorkers)
	var countersWG sync.WaitGroup
	countersWG.Add(numWorkers)

	builder := JobBuilderFunc(func() Job {
		workerID := atomic.AddInt32(&builds, 1)

		var count int
		buildsWG.Done()
		return JobFunc(func(ctx context.Context) error {
			// count is not shared across worker goroutines
			// no need to protect it against data races
			count++
			// same for the slice, since each worker
			// updated only its own index.
			counters[workerID-1] = count

			if count == 10 {
				countersWG.Done()
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		})
	})

	if err := p.StartWithBuilder(builder); err != nil {
		t.Fatalf("unexpected error starting poole. got %+v", err)
	}

	buildsWG.Wait()
	if int(builds) != numWorkers {
		t.Fatalf("unexpected nnumber of job builds, got %d, want: %d", builds, numWorkers)
	}

	countersWG.Wait()
	if err := p.CloseWithTimeout(time.Second); err != nil {
		t.Fatal("cannot stop pool", err)
	}

	for i, c := range counters {
		if c != 10 {
			t.Fatalf("unexpected counter for worker %d: got %d, want 10", i+1, c)
		}
	}
}

func TestPool_Run(t *testing.T) {
	var pool Pool

	ctx, cancel := context.WithCancel(context.Background())

	job := JobFunc(func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return nil
	})

	err := pool.Run(ctx, job)
	if err != nil {
		t.Fatal("unexpected error running the pool", err)
	}

	err = pool.Run(ctx, job)
	if !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("expected failure running a pool twice, got nil, want %v", ErrPoolClosed)
	}
}

func TestPool_RunStopOnErrors(t *testing.T) {
	pool, err := NewWithConfig(Config{StopOnErrors: true})
	if err != nil {
		t.Fatal("cannot build the pool", err)
	}

	dummyError := errors.New("some error")
	job := JobFunc(func(ctx context.Context) error {
		return dummyError
	})

	err = pool.Run(context.Background(), job)
	if !errors.Is(err, dummyError) {
		t.Fatalf("unexpected error running the pool, want %v got %v", dummyError, err)
	}
}

func TestPool_More(t *testing.T) {
	testCases := []struct {
		name        string
		pool        *Pool
		wantCurrent int
		wantErr     error
	}{
		{
			name: "max reached",
			pool: Must(NewWithConfig(Config{
				Max: 1,
			})),
			wantCurrent: 1,
			wantErr:     ErrMaxReached,
		},
		{
			name: "ok",
			pool: Must(NewWithConfig(Config{
				Max: 2,
			})),
			wantCurrent: 2,
			wantErr:     nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() {
				_ = tc.pool.CloseWithTimeout(time.Second)
			})
			if err := tc.pool.Start(dummyJob); err != nil {
				t.Fatalf("unexpected error starting pool: %+v", err)
			}

			got := tc.pool.More()
			if !errors.Is(got, tc.wantErr) {
				t.Fatalf("got error %+v, want: %+v", got, tc.wantErr)
			}
			if current := tc.pool.Current(); current != tc.wantCurrent {
				t.Fatalf("unexpected number of workers: got %+v, want: %+v", current, tc.wantCurrent)
			}
		})
	}
}

func TestPool_MoreErrors(t *testing.T) {
	t.Run("pool closed", func(t *testing.T) {
		var p Pool
		if err := p.Close(context.Background()); err != nil {
			t.Fatalf("unexpected error closing an uninitialized pool; got %+v", err)
		}

		if got, want := p.More(), ErrPoolClosed; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("pool not started", func(t *testing.T) {
		var p Pool
		if got, want := p.More(), ErrNotStarted; got != want {
			t.Fatalf("unexpected error starting pool for the second time; got %+v, want %+v", got, want)
		}
	})
}

func TestPool_Less(t *testing.T) {
	t.Run("reduce number of workers", func(t *testing.T) {
		var p Pool
		t.Cleanup(func() {
			if err := p.Close(context.Background()); err != nil {
				t.Fatal("cannot stop pool", err)
			}
		})

		if err := p.Start(dummyJob); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}
		if err := p.More(); err != nil {
			t.Fatalf("unexpected error adding workers: %+v", err)
		}
		if current := p.Current(); current != 2 {
			t.Fatalf("unexpected number of workers: got %d, want 2", current)
		}

		if got := p.Less(); got != nil {
			t.Fatalf("cannot remove one worker: got error %+v, want nil", got)
		}
		if got := p.Current(); got != 1 {
			t.Fatalf("unexpected number of workers: got %d, want 1", got)
		}
	})

	t.Run("error minimum number of workers reached", func(t *testing.T) {
		var p Pool
		t.Cleanup(func() {
			if err := p.CloseWithTimeout(time.Second); err != nil {
				t.Fatal("cannot stop pool", err)
			}
		})

		if err := p.Start(dummyJob); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}
		if current := p.Current(); current != 1 {
			t.Fatalf("unexpected number of workers: got %d, want 1", current)
		}

		if err := p.Less(); err != nil {
			t.Fatal("unexpected error removing last workers", err)
		}
		if got := p.Less(); got != ErrMinReached {
			t.Fatalf("got error %+v, want %v", got, ErrMinReached)
		}
		if got := p.Current(); got != 0 {
			t.Fatalf("unexpected number of workers: got %d, want 1", got)
		}
	})

	t.Run("error pool closed", func(t *testing.T) {
		var p Pool
		if err := p.Close(context.Background()); err != nil {
			t.Fatalf("unexpected error closing an uninitialized pool; got %+v", err)
		}

		if got, want := p.Less(), ErrPoolClosed; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("error pool not started", func(t *testing.T) {
		var p Pool
		if got, want := p.Less(), ErrNotStarted; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})
}

func TestPool_Close(t *testing.T) {
	t.Run("close successfully a non started pool", func(t *testing.T) {
		var p Pool
		got := p.Close(context.Background())
		if !errors.Is(got, nil) {
			t.Fatalf("unexpected error: %+v, want nil", got)
		}
	})

	t.Run("close successfully a started pool", func(t *testing.T) {
		var p Pool
		if err := p.Start(dummyJob); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}

		got := p.CloseWithTimeout(time.Second)
		if !errors.Is(got, nil) {
			t.Fatalf("unexpected error closing pool: %+v, want nil", got)
		}
	})

	t.Run("close timeout error", func(t *testing.T) {
		var p Pool
		running := make(chan struct{})
		job := JobFunc(func(_ context.Context) error {
			// signal that we are running the job
			running <- struct{}{}
			// block the job so the call to close times out
			running <- struct{}{}
			return nil
		})
		if err := p.Start(job); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}

		<-running
		got := p.CloseWithTimeout(25 * time.Millisecond)
		if !errors.Is(got, context.DeadlineExceeded) {
			t.Fatalf("unexpected error closing pool: %+v, want %+v", got, context.DeadlineExceeded)
		}
		<-running

		got = p.Close(context.Background())
		if !errors.Is(got, ErrPoolClosed) {
			t.Fatalf("unexpected error closing pool for the second time: %+v, want %+v", got, ErrPoolClosed)
		}
	})

	t.Run("close cancelled error", func(t *testing.T) {
		var p Pool
		job := JobFunc(func(_ context.Context) error {
			block := make(chan struct{})
			<-block
			return nil
		})
		if err := p.Start(job); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got := p.Close(ctx)
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("unexpected error closing pool: %+v, want %+v", got, context.Canceled)
		}
	})
}

func TestPool_ConcurrencySafety(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var (
		count         uint32
		headStart     = 100
		total         = 300 + headStart
		startRemoving = make(chan struct{})
	)

	var p Pool
	job := JobFunc(func(ctx context.Context) error {
		if atomic.AddUint32(&count, 1) == uint32(headStart) {
			close(startRemoving)
		}
		select {
		case <-ctx.Done():
		case <-time.NewTimer(100 * time.Millisecond).C:
		}
		return nil
	})

	if err := p.Start(job); err != nil {
		t.Fatal("cannot start pool", err)
	}

	errs := make(chan error, 2)

	go func(t *testing.T) {
		for i := 0; i < rand.Intn(total); i++ {
			if err := p.More(); err != nil {
				errs <- fmt.Errorf("cannot add worker: %v", err)
				return
			}
		}
		errs <- nil
	}(t)

	go func(t *testing.T) {
		<-startRemoving
		for i := 0; i < rand.Intn(total); i++ {
			if err := p.Less(); err != nil && !errors.Is(err, ErrMinReached) {
				errs <- fmt.Errorf("cannot remove worker: %v", err)
				return
			}
		}
		errs <- nil
	}(t)

	for i := 0; i < 2; i++ {
		err := <-errs
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := p.Close(ctx); err != nil {
		t.Fatal("cannot close pool", err)
	}
}

func BenchmarkPool(b *testing.B) {
	tests := []struct {
		name     string
		count    int
		skipPool bool
	}{
		{
			name:  "10",
			count: 10,
		},
		{
			name:  "100",
			count: 100,
		},
		{
			name:  "1K",
			count: 1000,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var p Pool
				if err := p.Start(slowJob); err != nil {
					b.Fatal("cannot start pool", err)
				}
				for j := 0; j < tc.count; j++ {
					if err := p.More(); err != nil {
						b.Fatal("cannot add worker", err)
					}
					if math.Mod(float64(j+1), 10) == 0 {
						if err := p.Less(); err != nil {
							b.Fatal("cannot remove worker", err)
						}
					}
				}
				if err := p.Close(ctx); err != nil {
					b.Fatal("cannot close pool", err)
				}
			}
		})
	}
}

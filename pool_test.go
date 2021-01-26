package workers

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	dummyJob = func(_ context.Context) {}
	slowJob  = func(_ context.Context) {
		<-time.NewTimer(150 * time.Millisecond).C
	}
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
			_, got := NewWithConfig(dummyJob, tc.config)
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
		if !errors.Is(err.(error), ErrInvalidMin) {
			t.Fatalf("unexpected panic error; got %+v, want %v", err, ErrInvalidMin)
		}
	}()

	Must(NewWithConfig(dummyJob, Config{Min: -1}))
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
			p := Must(NewWithConfig(dummyJob, tc.config))
			t.Cleanup(func() {
				if err := p.CloseWIthTimeout(time.Second); err != nil {
					t.Fatal("cannot stop pool", err)
				}
			})

			if err := p.Start(); err != nil {
				t.Fatalf("unexpected error starting poole. got %+v", err)
			}
			if got := p.Current(); got != tc.want {
				t.Fatalf("unexpected initial workers. got %d, want: %d", got, tc.want)
			}
		})
	}
}

func TestPool_StartErrors(t *testing.T) {
	t.Run("pool not configured", func(t *testing.T) {
		var p Pool
		if got, want := p.Start(), ErrNotConfigured; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("pool closed", func(t *testing.T) {
		p := Must(New(dummyJob))

		if err := p.CloseWIthTimeout(time.Second); err != nil {
			t.Fatalf("unexpected error closing an uninitialized pool; got %+v", err)
		}

		if got, want := p.Start(), ErrPoolClosed; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("pool already started", func(t *testing.T) {
		p := Must(New(dummyJob))

		if err := p.Start(); err != nil {
			t.Fatalf("unexpected error starting for the first time; got %+v", err)
		}

		if got, want := p.Start(), ErrPoolStarted; got != want {
			t.Fatalf("unexpected error starting pool for the second time; got %+v, want %+v", got, want)
		}
	})
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
			pool: Must(NewWithConfig(dummyJob, Config{
				Max: 1,
			})),
			wantCurrent: 1,
			wantErr:     ErrMaxReached,
		},
		{
			name: "ok",
			pool: Must(NewWithConfig(dummyJob, Config{
				Max: 2,
			})),
			wantCurrent: 2,
			wantErr:     nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() {
				_ = tc.pool.CloseWIthTimeout(time.Second)
			})
			if err := tc.pool.Start(); err != nil {
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
	t.Run("pool not configured", func(t *testing.T) {
		var p Pool
		if got, want := p.More(), ErrNotConfigured; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("pool closed", func(t *testing.T) {
		p := Must(New(dummyJob))

		if err := p.Close(context.Background()); err != nil {
			t.Fatalf("unexpected error closing an uninitialized pool; got %+v", err)
		}

		if got, want := p.More(), ErrPoolClosed; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("pool not started", func(t *testing.T) {
		p := Must(New(dummyJob))

		if got, want := p.More(), ErrNotStarted; got != want {
			t.Fatalf("unexpected error starting pool for the second time; got %+v, want %+v", got, want)
		}
	})
}

func TestPool_Less(t *testing.T) {
	t.Run("reduce number of workers", func(t *testing.T) {
		p := Must(New(dummyJob))
		t.Cleanup(func() {
			if err := p.Close(context.Background()); err != nil {
				t.Fatal("cannot stop pool", err)
			}
		})

		if err := p.Start(); err != nil {
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

	t.Run("error returned if trying to go below the minimum number of workers", func(t *testing.T) {
		p := Must(New(dummyJob))
		t.Cleanup(func() {
			if err := p.CloseWIthTimeout(time.Second); err != nil {
				t.Fatal("cannot stop pool", err)
			}
		})

		if err := p.Start(); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}
		if current := p.Current(); current != 1 {
			t.Fatalf("unexpected number of workers: got %d, want 1", current)
		}

		if got := p.Less(); got != ErrMinReached {
			t.Fatalf("got error %+v, want %v", got, ErrMinReached)
		}
		if got := p.Current(); got != 1 {
			t.Fatalf("unexpected number of workers: got %d, want 1", got)
		}
	})

	t.Run("error pool not configured", func(t *testing.T) {
		var p Pool
		if got, want := p.Less(), ErrNotConfigured; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})

	t.Run("error pool closed", func(t *testing.T) {
		p := Must(New(dummyJob))

		if err := p.Close(context.Background()); err != nil {
			t.Fatalf("unexpected error closing an uninitialized pool; got %+v", err)
		}

		if got, want := p.Less(), ErrPoolClosed; got != want {
			t.Fatalf("unexpected error starting pool; got %+v, want %+v", got, want)
		}
	})
}

func TestPool_Close(t *testing.T) {
	t.Run("close successfully a non started pool", func(t *testing.T) {
		p := Must(New(dummyJob))
		got := p.Close(context.Background())
		if !errors.Is(got, nil) {
			t.Fatalf("unexpected error: %+v, want nil", got)
		}
	})

	t.Run("close successfully a started pool", func(t *testing.T) {
		p := Must(New(dummyJob))
		if err := p.Start(); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}

		got := p.CloseWIthTimeout(time.Second)
		if !errors.Is(got, nil) {
			t.Fatalf("unexpected error closing pool: %+v, want nil", got)
		}
	})

	t.Run("close timeout error", func(t *testing.T) {
		running := make(chan struct{})
		p := Must(New(func(_ context.Context) {
			// signal that we are running the job
			running <- struct{}{}
			// block the job so the call to close times out
			running <- struct{}{}
		}))
		if err := p.Start(); err != nil {
			t.Fatalf("unexpected error starting pool: %+v", err)
		}

		<-running
		got := p.CloseWIthTimeout(25 * time.Millisecond)
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
		p := Must(New(func(_ context.Context) {
			block := make(chan struct{})
			<-block
		}))
		if err := p.Start(); err != nil {
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
				p := Must(New(slowJob))
				if err := p.Start(); err != nil {
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

	p := Must(New(func(ctx context.Context) {
		if atomic.AddUint32(&count, 1) == uint32(headStart) {
			close(startRemoving)
		}
		select {
		case <-ctx.Done():
		case <-time.NewTimer(100 * time.Millisecond).C:
		}
	}))

	if err := p.Start(); err != nil {
		t.Fatal("cannot start pool", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		for i := 0; i < rand.Intn(total); i++ {
			if err := p.More(); err != nil {
				t.Fatal("cannot add worker", err)
			}
		}
		wg.Done()
	}()

	go func() {
		<-startRemoving
		for i := 0; i < rand.Intn(total); i++ {
			if err := p.Less(); err != nil && !errors.Is(err, ErrMinReached) {
				t.Fatal("cannot remove worker", err)
			}
		}
		wg.Done()
	}()

	wg.Wait()
	if err := p.Close(ctx); err != nil {
		t.Fatal("cannot close pool", err)
	}
}

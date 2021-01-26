package workers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrPoolClosed     = errors.New("pool is closed")
	ErrPoolStarted    = errors.New("pool already started")
	ErrNotStarted     = errors.New("pool has not started")
	ErrNotConfigured  = errors.New("pool not configured")
	ErrInvalidMax     = errors.New("maximum workers must be equal or greater than minimum")
	ErrInvalidMin     = errors.New("minimum workers must be at least one")
	ErrInvalidInitial = errors.New("initial workers must match at least the minimum")
	ErrMinReached     = errors.New("minimum number of workers reached")
	ErrMaxReached     = errors.New("maximum number of workers reached")
)

// Job is a function that does work.
//
// The only parameter that will receive is a context, the job
// should try to honor the context cancellation signal as soon
// as possible.
//
// The context will be cancelled when removing workers from
// the pool or stopping the pool completely.
type Job = func(ctx context.Context)

// JobMiddleware is a function that wraps the job and can
// be used to extend the functionality of the pool.
type JobMiddleware = func(job Job) Job

// Config allows to configure the number of workers
// that will be running in the pool.
type Config struct {
	// Min indicates the minimum number of workers that can run concurrently.
	// When 0 is given the minimum is defaulted to 1.
	Min int

	// Max indicates the maximum number of workers that can run concurrently.
	// the default "0" indicates an infinite number of workers.
	Max int

	// Initial indicates the initial number of workers that should be running.
	// When 0 is given the minimum is used.
	Initial int
}

// New creates a new pool with the default configuration.
//
// It accepts an arbitrary number of job middlewares to run.
func New(job Job, middlewares ...JobMiddleware) (*Pool, error) {
	return NewWithConfig(job, Config{}, middlewares...)
}

// NewWithConfig creates a new pool with an specific configuration.
//
// It accepts an arbitrary number of job middlewares to run.
func NewWithConfig(job Job, cfg Config, middlewares ...JobMiddleware) (*Pool, error) {
	if cfg.Min == 0 {
		cfg.Min = 1
	}
	if cfg.Initial == 0 {
		cfg.Initial = cfg.Min
	}

	if cfg.Min < 1 {
		return nil, fmt.Errorf("%w: min %d", ErrInvalidMin, cfg.Min)
	}
	if cfg.Max != 0 && cfg.Min > cfg.Max {
		return nil, fmt.Errorf("%w: max: %d, min %d", ErrInvalidMax, cfg.Max, cfg.Min)
	}
	if cfg.Initial < cfg.Min {
		return nil, fmt.Errorf("%w: initial: %d, min %d", ErrInvalidInitial, cfg.Initial, cfg.Min)
	}

	for _, mw := range middlewares {
		job = mw(job)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		job:     job,
		cfg:     &cfg,
		ctx:     ctx,
		cancel:  cancel,
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}

	return p, nil
}

// Must checks if the result of creating a pool
// has failed and if so, panics.
func Must(p *Pool, err error) *Pool {
	if err != nil {
		panic(err)
	}
	return p
}

// Pool is a pool of workers that can be started
// to run a job non-stop concurrently.
type Pool struct {
	job     Job
	cfg     *Config
	workers []*worker

	// Current pool state.
	started bool
	closed  bool

	// Pool context that will be the
	// parent ctx for the workers.
	ctx    context.Context
	cancel func()

	// Holds the number of running workers
	// When a worker stops it will send a
	// signal the channel.
	running uint64
	stopped chan struct{}
	done    chan struct{}

	mx sync.RWMutex
}

// Start launches the workers and keeps them running until the pool is closed.
func (p *Pool) Start() error {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.cfg == nil {
		return ErrNotConfigured
	}
	if p.closed {
		return ErrPoolClosed
	}
	if p.started {
		return ErrPoolStarted
	}
	p.started = true

	for i := 0; i < p.cfg.Initial; i++ {
		p.workers = append(p.workers, p.newWorker())
	}

	go func() {
		for range p.stopped {
			if atomic.AddUint64(&p.running, ^uint64(0)) == 0 {
				close(p.done)
				close(p.stopped)
			}
		}
	}()

	return nil
}

// More starts a new worker in the pool.
func (p *Pool) More() error {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.cfg == nil {
		return ErrNotConfigured
	}
	if p.closed {
		return ErrPoolClosed
	}
	if !p.started {
		return ErrNotStarted
	}
	if p.cfg.Max != 0 && len(p.workers) == p.cfg.Max {
		return ErrMaxReached
	}

	p.workers = append(p.workers, p.newWorker())

	return nil
}

// Less signals the pool to reduce a worker and
// returns immediately without waiting for the
// worker to stop, which will eventually happen.
func (p *Pool) Less() error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.StopOne(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}

// StopOne stops one worker and removes it from the pool.
//
// If the number of workers is already the minimum the call
// will return "ErrMinReached" error.
//
// The current number of workers will decrement even if the
// given context is cancelled or times out. The worker may still
// be executing the job but it has a pending signal to terminate
// and will eventually stop.
func (p *Pool) StopOne(ctx context.Context) error {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.cfg == nil {
		return ErrNotConfigured
	}
	if p.closed {
		return ErrPoolClosed
	}
	current := len(p.workers)
	if current == p.cfg.Min {
		return ErrMinReached
	}

	// pop the last worker. We can remove it since
	// we're going to call stop on the worker, and
	// whether stops before the context is cancelled
	// or not, is irrelevant, the "quit" will be
	// sent and sooner or later the worker will stop.
	w := p.workers[current-1]
	p.workers = p.workers[:current-1]

	return w.stop(ctx)
}

// Current returns the current number of workers.
//
// There may be more workers executing job while they are
// pending to complete it's last job. See Less for
// an explanation why.
func (p *Pool) Current() int {
	p.mx.RLock()
	defer p.mx.RUnlock()

	return len(p.workers)
}

// Close stops all the workers and closes the pool.
//
// Only the first call to Close will shutdown the pool,
// the next calls will be ignored and return nil.
func (p *Pool) Close(ctx context.Context) error {
	p.mx.RLock()
	defer p.mx.RUnlock()

	if p.closed {
		return ErrPoolClosed
	}

	// close the pool to prevent
	// new workers to be added.
	p.closed = true

	// early exit if the pool
	// is not running.
	if !p.started {
		return nil
	}

	// cancel the pool context,
	// all workers will stop.
	p.cancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return nil
	}
}

// CloseWIthTimeout displays the same behaviour as close, but
// instead of passing a context for cancellation we can pass
// a timeout value.
func (p *Pool) CloseWIthTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return p.Close(ctx)
}

func (p *Pool) newWorker() *worker {
	ctx, cancel := context.WithCancel(p.ctx)
	w := &worker{
		done:   make(chan struct{}),
		cancel: cancel,
	}

	atomic.AddUint64(&p.running, 1)
	go w.work(ctx, p.job, p.stopped)

	return w
}

type worker struct {
	job      func()
	interval time.Duration
	cancel   func()
	done     chan struct{}
}

func (w *worker) work(ctx context.Context, job Job, stopped chan<- struct{}) {
	defer func() {
		close(w.done)
		stopped <- struct{}{}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			job(ctx)
		}
	}
}

func (w *worker) stop(ctx context.Context) error {
	w.cancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return nil
	}
}

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

// Job represent some work that needs to be done non-stop.
type Job interface {
	// Do executes the job.
	//
	// The only parameter that will receive is a context, the job
	// should try to honor the context cancellation signal as soon
	// as possible.
	//
	// The context will be cancelled when removing workers from
	// the pool or stopping the pool completely.
	Do(ctx context.Context)
}

// JobFunc is a helper function that is a job.
type JobFunc func(ctx context.Context)

// Do executes the job work.
func (f JobFunc) Do(ctx context.Context) {
	f(ctx)
}

// Middleware is a function that wraps the job and can
// be used to extend the functionality of the pool.
type Middleware interface {
	Wrap(job Job) Job
}

// MiddlewareFunc is a function that implements the
// job middleware interface.
type MiddlewareFunc func(job Job) Job

// Wrap executes the middleware function wrapping the job.
func (f MiddlewareFunc) Wrap(job Job) Job {
	return f(job)
}

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
func New(job Job, middlewares ...Middleware) (*Pool, error) {
	return NewWithConfig(job, Config{}, middlewares...)
}

// NewWithConfig creates a new pool with an specific configuration.
//
// It accepts an arbitrary number of job middlewares to run.
func NewWithConfig(job Job, cfg Config, middlewares ...Middleware) (*Pool, error) {
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
		job = mw.Wrap(job)
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

// Less removes the number of workers in the pool.
//
// This call does not wait for the worker to finish
// its current job, if the pool is closed though,
// the call to close it will wait for all removed
// workers to finish before returning.
func (p *Pool) Less() error {
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

	// pop the last worker
	w := p.workers[current-1]
	p.workers = p.workers[:current-1]

	// stop the worker
	w.cancel()

	return nil
}

// Current returns the current number of workers.
//
// There may be more workers executing jobs while
// they are to complete it's last job after being
// removed, but they will eventually finish and
// stop processing new jobs.
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

// CloseWIthTimeout closes the pool waiting
// for a certain amount of time.
func (p *Pool) CloseWIthTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return p.Close(ctx)
}

func (p *Pool) newWorker() *worker {
	ctx, cancel := context.WithCancel(p.ctx)
	w := &worker{
		cancel: cancel,
	}

	atomic.AddUint64(&p.running, 1)
	go w.work(ctx, p.job, p.stopped)

	return w
}

type worker struct {
	cancel func()
}

func (w *worker) work(ctx context.Context, job Job, stopped chan<- struct{}) {
	defer func() {
		stopped <- struct{}{}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			job.Do(ctx)
		}
	}
}

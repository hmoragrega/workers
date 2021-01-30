package workers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrPoolClosed is triggered when trying to start and add or remove
	// workers from the pool after closing it.
	ErrPoolClosed = errors.New("pool is closed")

	// ErrPoolStarted is triggered when trying to start the pool when it's
	// already running.
	ErrPoolStarted = errors.New("pool already started")

	// ErrNotStarted is returned when trying to add or remove workers from
	// the pool after closing it.
	ErrNotStarted = errors.New("pool has not started")

	// ErrInvalidMax is triggered when configuring a pool with an invalid
	// maximum number of workers.
	ErrInvalidMax = errors.New("the maximum is less than the minimum workers")

	// ErrInvalidMin is triggered when configuring a pool with an invalid
	// minimum number of workers.
	ErrInvalidMin = errors.New("negative number of minimum workers")

	// ErrInvalidInitial is triggered when configuring a pool with an invalid
	// initial number of workers.
	ErrInvalidInitial = errors.New("the initial is less the minimum workers")

	// ErrMinReached is triggered when trying to remove a worker when the
	// pool is already running at minimum capacity.
	ErrMinReached = errors.New("minimum number of workers reached")

	// ErrMaxReached is triggered when trying to add a worker when the
	// pool is already running at maximum capacity.
	ErrMaxReached = errors.New("maximum number of workers reached")
)

// Job represents some work that needs to be done non-stop.
type Job interface {
	// Do executes the job.
	//
	// The only parameter that will receive is the worker context,
	// the job should try to honor the context cancellation signal
	// as soon as possible.
	//
	// The context will be cancelled when removing workers from
	// the pool or stopping the pool completely
	Do(ctx context.Context) error
}

// JobFunc is a helper function that is a job.
type JobFunc func(ctx context.Context) error

// Do executes the job work.
func (f JobFunc) Do(ctx context.Context) error {
	return f(ctx)
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

// Wrap is a helper to apply a chain of middleware to a job.
func Wrap(job Job, middlewares ...Middleware) Job {
	for _, mw := range middlewares {
		job = mw.Wrap(job)
	}
	return job
}

// Config allows to configure the number of workers
// that will be running in the pool.
type Config struct {
	// Min indicates the minimum number of workers that can run concurrently.
	// By default the pool can have 0 workers, pausing it effectively.
	Min int

	// Max indicates the maximum number of workers that can run concurrently.
	// the default "0" indicates an infinite number of workers.
	Max int

	// Initial indicates the initial number of workers that should be running.
	// The default value will be the greater number between 1 or the given minimum.
	Initial int
}

// New creates a new pool with the default configuration.
//
// It accepts an arbitrary number of job middlewares to run.
func New(middlewares ...Middleware) *Pool {
	return newDefault(middlewares...)
}

// NewWithConfig creates a new pool with an specific configuration.
//
// It accepts an arbitrary number of job middlewares to run.
func NewWithConfig(cfg Config, middlewares ...Middleware) (*Pool, error) {
	if cfg.Initial == 0 {
		cfg.Initial = defaultInitial
	}
	if cfg.Min < 0 {
		return nil, fmt.Errorf("%w: min %d", ErrInvalidMin, cfg.Min)
	}
	if cfg.Max != 0 && cfg.Max < cfg.Min {
		return nil, fmt.Errorf("%w: max: %d, min %d", ErrInvalidMax, cfg.Max, cfg.Min)
	}
	if cfg.Initial < cfg.Min {
		return nil, fmt.Errorf("%w: initial: %d, min %d", ErrInvalidInitial, cfg.Initial, cfg.Min)
	}

	return &Pool{
		min:     cfg.Min,
		max:     cfg.Max,
		initial: cfg.Initial,
		mws:     middlewares,
	}, nil
}

// Must checks if the result of creating a pool
// has failed and if so, panics.
func Must(p *Pool, err error) *Pool {
	if err != nil {
		panic(err)
	}
	return p
}

const (
	defaultMin     = 0
	defaultMax     = 0
	defaultInitial = 1
)

func newDefault(middlewares ...Middleware) *Pool {
	return &Pool{
		min:     defaultMin,
		max:     defaultMax,
		initial: defaultInitial,
		mws:     middlewares,
	}
}

// Pool is a pool of workers that can be started
// to run a job non-stop concurrently.
type Pool struct {
	// config
	min     int
	initial int
	max     int

	// job and its workers.
	job     Job
	mws     []Middleware
	workers []*worker

	// Current pool state.
	started bool
	closed  bool

	// Pool context that will be the
	// parent ctx for the workers.
	ctx    context.Context
	cancel func()

	// workers will let the pool know
	// when they start and when they stop
	// through this channel (+1, -1)
	running chan int

	// Once the pool is closed and all the
	// workers stopped this channel
	// will be closed to signal the pool
	// has finished in a clean way.
	done chan struct{}

	mx sync.RWMutex
}

// Start launches the workers and keeps them running until the pool is closed.
func (p *Pool) Start(job Job) error {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.closed {
		return ErrPoolClosed
	}
	if p.started {
		return ErrPoolStarted
	}
	initial := p.initial
	if initial == 0 {
		initial = 1
	}

	p.started = true
	p.job = Wrap(job, p.mws...)
	p.running = make(chan int)
	p.done = make(chan struct{})
	p.workers = make([]*worker, initial)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	go p.waitForWorkersToStop()

	var wg sync.WaitGroup
	wg.Add(initial)
	for i := 0; i < initial; i++ {
		go func(i int) {
			p.workers[i] = p.newWorker()
			wg.Done()
		}(i)
	}

	wg.Wait()
	return nil
}

func (p *Pool) waitForWorkersToStop() {
	var running int
	defer func() {
		close(p.done)
		close(p.running)
	}()
	for {
		select {
		case delta := <-p.running:
			running += delta
		case <-p.ctx.Done():
			// we may receive the pool close cancellation after
			// all workers have already stop, we need to
			// fallthrough
		}
		if running > 0 {
			continue
		}
		// we need to understand if we have no workers
		// because the pool is closed, or because the
		// all workers were removed (paused)
		p.mx.RLock()
		isClosed := p.closed
		p.mx.RUnlock()
		if isClosed {
			return
		}
	}
}

// More starts a new worker in the pool.
func (p *Pool) More() error {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.closed {
		return ErrPoolClosed
	}
	if !p.started {
		return ErrNotStarted
	}
	if p.max != 0 && len(p.workers) == p.max {
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

	if p.closed {
		return ErrPoolClosed
	}
	if !p.started {
		return ErrNotStarted
	}
	current := len(p.workers)
	if current == p.min {
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
	if err := p.close(); err != nil {
		return err
	}
	if !p.hasStarted() {
		return nil
	}

	p.cancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return nil
	}
}

// close attempts to close the pool.
func (p *Pool) hasStarted() bool {
	p.mx.RLock()
	defer p.mx.RUnlock()

	return p.started
}

// close attempts to close the pool.
func (p *Pool) close() error {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.closed {
		return ErrPoolClosed
	}
	p.closed = true

	return nil
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

	p.running <- 1
	go func() {
		defer func() {
			p.running <- -1
		}()
		w.work(ctx, p.job)
	}()

	return w
}

type worker struct {
	cancel func()
}

func (w *worker) work(ctx context.Context, job Job) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_ = job.Do(ctx)
		}
	}
}

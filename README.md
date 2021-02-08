# workers

[![godoc][godoc-badge]][godoc-url]
[![ci][ci-badge]][ci-url]
[![coverage][coverage-badge]][coverage-url]
[![goreport][goreport-badge]][goreport-url]

Go package that allows to run a pool of workers to run a job concurrently in the background.

## Usage
Create a pool and start running a job.

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/hmoragrega/workers"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	go func() {
		<-stop
		cancel()
	}()

	job := workers.JobFunc(func(ctx context.Context) error {
		// my job code 
		return nil
	})

	var pool workers.Pool

	if err := pool.Run(ctx, job); err != nil {
		log.Fatal("job pool failed", err)
	}
}
```

### Pool
A pool runs a single job trough a number of concurrent workers.

By default, a pool will have one worker, and will allow to increase
the number of workers indefinitely or even remove them all to pause the job.

There are a few configuration parameters that can tweak the pool
behaviour

```go
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
```

To have a pool with a tweaked config you can call `NewWithConfig`
```go
pool := workers.Must(NewWithConfig(workers.Config{
	Min:     3,
	Max:     10,
	Initial: 5,
}))
```

#### Running non-stop
If you want to keep the pool running in a blocking call you
 can call `Run` or `RunWithBuilder`.   
   
In this mode the pool will run until the given context is 
terminated. 

The pool then will be closed without a timeout.

```go
var pool workers.Pool

if err := pool.Run(ctx, job); err != nil {
	log.Println("pool ended with error", err)
}
```

Alternatively you can start and stop the pool to have 
a fine-grained control of the pool live-cycle.

#### Starting the pool
To start the pool give it a job (or a [job builder](#builder-jobs)) to run:
```go
if err := pool.Start(job); err != nil {
	log.Println("cannot start the pool", err)
}
```
The operation will fail if:
- the pool is has already been closed.
- the pool is already running.

#### Stopping the pool
Stopping the pool will request all the workers stop and 
wait until all of them finish its ongoing jobs, or the 
given context is cancelled.

```go
if err := pool.Close(ctx); err != nil {
	log.Println("cannot start the pool", err)
}
```
 
`Close` will also wait for workers that were removed but
are still executing its last job 

The operation will fail if:
- the pool is has already been closed.
- the pool is not running

Alternative `CloseWithTimeout` can be used passing a
 `time.Duration` as a helper.

#### Adding workers
To add a new worker to the pool you can call
```go
if err := pool.More(); err != nil {
	log.Println("cannot add more workers", err)
}
```
The operation will fail if:
- the maximum number of workers has been reached
- the pool has already been closed
- the pool is not running

#### Removing workers
To remove a worker you can use `Less`. 
```go
if err := pool.Less(); err != nil {
	log.Println("cannot remove more workers", err)
}
```
Less will remove a worker from the pool, immediately reducing
the number of workers running, and request the worker to 
stop processing jobs, but it won't wait until the worker 
finally stops. 

Note: the pool will wait until all jobs finish, even those
from those workers that were removed.

The operation will fail if:
- the minimum number of workers has been reached
- the pool has already been closed
- the pool is not running

### Job
A job represents a task that needs to be performed.
```go
// Job represent some work that needs to be done.
type Job interface {
	// Do executes the job.
	//
	// The only parameter that will receive is a context, the job
	// should try to honor the context cancellation signal as soon
	// as possible.
	//
	// The context will be cancelled when removing workers from
	// the pool or stopping the pool completely.
	Do(ctx context.Context) error
}
```

Simple jobs can use the helper `JobFunc` to comply with the interface   
```go
// JobFunc is a helper function that is a job.
type JobFunc func(ctx context.Context) error
```

To extend the functionality of jobs you can use builders, middlewares
or wrappers.

#### Builder Jobs
Sometimes you want to share data across job execution within the same worker.

For this you can use a job builder to indicate that every worker that 
joins the pool will have its own job.   

**NOTE:** in this case, the jobs are not going to be running concurrently, 
making much easier to avoid data races.     

```go
// JobBuilder is a job that needs to be built during
// the initialization for each worker.
type JobBuilder interface {
	// New generates a new job for a new worker.
	New() Job
}
```
You will have to call `StartWithBuilder` method to start the pool with a job builder.
```go
var (
	numberOfWorkers = 3
	workSlots       = make([]int, numberOfWorkers)	
	workerID int32
)

builder := JobBuilderFunc(func() Job {
	id := atomic.AddInt32(&workerID, 1)

	var count int
	return JobFunc(func(ctx context.Context) error {
		// count is not shared across worker goroutines
		// no need to protect it against data races
		count++
		// same for the slice, since each worker
		// updated only its own index.
		workSlots[id-1] = count
		return nil
   	})
})

p.StartWithBuilder(builder)
```

#### Job Middleware 
A middleware allows to extend the job capabilities
```go
// Middleware is a function that wraps the job and can
// be used to extend the functionality of the pool.
type Middleware interface {
	Wrap(job Job) Job
}
```

The helper `MiddlewareFunc` can be used to wrap
simple middleware functions
```go 
// MiddlewareFunc is a function that implements the
// job middleware interface.
type MiddlewareFunc func(job Job) Job
```

Some example of middleware:
* [Counter](middleware/counter.go) counts how many jobs start, finish and fail.
* [Elapsed](middleware/elapsed.go) extends the counter middleware providing also:
  - the total amount of time.
  - the average time.
  - the time of the last executed job.
* [Retry](middleware/retry.go) it will retry failed jobs a certain amount of times. 
* [Wait](middleware/wait.go) allows to add a pause between job executions. (Job will
still be running concurrently if there are more workers).

As an exercise let's log the job result with our favourite logging library.
```go
// loggerMiddleware is a middleware that logs the result of a job
// with "debug" or "error" level depending on the result.
loggerMiddleware := func(name string) workers.MiddlewareFunc {
	return func(job workers.Job) workers.Job {
		return workers.JobFunc(func(ctx context.Context) error {
			err := job.Do(ctx)

			logger.Cond(err != nil, logger.Error, logger.Debug).
				Log("job executed", "job", jobName, "error", err)

			return err
		})
	}
}

pool := workers.New(loggerMiddleware("my-job"))
pool.Start(workers.JobFunc(func(ctx context.Context) error {
	return someWork()
}))
```

If you need to add a middleware before starting the job instead of on pool creating
there's the little handy function `Wrap` that will easy applying them for you.

```go
// Wrap is a helper to apply a chain of middleware to a job.
func Wrap(job Job, middlewares ...Middleware) Job
```

```go
var pool Pool

job := workers.JobFunc(func(ctx context.Context) (err error) {
  	// work
	return err
}

pool.Start(workers.Wrap(job, jobLogger("my-job")))
```

#### Job Wrapper
A job wrapper can be used to change the signature of a job.

For example, your job may never return errors, yet still has to comply
with the Job interface that requires it.

You could use the [NoError wrapper](wrapper/no_error.go) to avoid
having to return error from you job function.

```go
job := func(ctx context.Context) {
	// work
}

var pool workers.Pool 
pool.Start(workers.NoError(job))
```


[ci-badge]: https://github.com/hmoragrega/workers/workflows/CI/badge.svg
[ci-url]:   https://github.com/hmoragrega/workers/actions?query=workflow%3ACI

[coverage-badge]: https://coveralls.io/repos/github/hmoragrega/workers/badge.svg?branch=main
[coverage-url]:   https://coveralls.io/github/hmoragrega/workers?branch=main

[godoc-badge]: https://pkg.go.dev/badge/github.com/hmoragrega/workers.svg
[godoc-url]:   https://pkg.go.dev/github.com/hmoragrega/workers

[goreport-badge]: https://goreportcard.com/badge/github.com/hmoragrega/workers
[goreport-url]: https://goreportcard.com/report/github.com/hmoragrega/workers

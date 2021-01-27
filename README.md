# workers

[![ci][ci-badge]][ci-url]
[![coverage][coverage-badge]][coverage-url]
[![godoc][godoc-badge]][godoc-url]

Go package that allows to run a pool of workers to run a job concurrently in the background.

## Usage

Create a pool of workers passing a job and start the pool.

```go
package main

import (
    "log"
    "context"
    "time"

    "github.com/hmoragrega/workers"
)

func main() {
    job := workers.JobFunc(func(ctx context.Context) {
        // my job code 
    })

    pool := workers.Must(workers.New(job))

    if err := pool.Start(); err != nil {
        log.Fatal("cannot start pool", err)
    }

    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        if err := pool.Close(ctx); err != nil {
            log.Fatal("cannot close pool", err)
        }    
    }()
}
```

### Pool
A pool runs a single job trough a number of concurrent workers.

By default, a pool will have one worker, and will allow to increase
the number of workers indefinitely. 

There are a few configuration parameters that can tweak the pool
behaviour

```go
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
```

To have a pool with a tweaked config you can call `NewWithConfig`
```go
pool, err := workers.NewWithConfig(job, workers.Config{
   Min:     3,
   Max:     10,
   Initial: 5,
})
```

#### Starting the pool
The pool won't process any job until it is started
```go
if err := pool.Start(); err != nil {
    log.Println("cannot start the pool", err)
}
```
The operation will fail if:
- the pool is not configured; `New` was not called
- the pool is closed; `Close` was called
- the pool is already running; `Start` was already called

#### Stopping the pool
Stopping the pool will request all the workers stop and 
wait until all of them finish its ongoing jobs or the 
given context is cancelled.

```go
if err := pool.Close(ctx); err != nil {
    log.Println("cannot start the pool", err)
}
```
 
`Close` will also wait for workers that were removed but
are still executing its last job 

The operation will fail if:
- the pool is not configured; `New` was not called
- the pool is already closed; `Close` was already called
- the pool is not running; `Start` was not called.

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
- the pool is not configured; `New` was not called
- the pool is closed; `Close` was called
- the pool is not running; `Start` was not called

#### Removing workers
To remove a worker you can use `Less`. 
```go
if err := pool.Less(); err != nil {
    log.Println("cannot remove more workers", err)
}
```
Less will remove a worker from the poo, immediately reduce
the number of worker running, and request the worker to 
stop processing jobs, but it won't wait until the worker 
finally stops.

These stopping workers will still be taken into account
when the closing the pool, waiting for them to finish.

The operation will fail if:
- the minimum number of workers has been reached
- the pool is not configured; `New` was not called
- the pool is closed; `Close` was called
- the pool is not running; `Start` was not called

### Job
A job is a simple function that accepts only one parameter, the worker context.

```go
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
```

Simple jobs can use the helper `JobFunc` to comply with the interface   
```go
// JobFunc is a helper function that is a job.
type JobFunc func(ctx context.Context)
```

There are two ways of extending the job functionality

#### Job Middleware 
A middleware allows to extend the job capabilities
```go
// Middleware is a function that wraps the job and can
// be used to extend the functionality of the pool.
type Middleware interface {
	Next(job Job) Job
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
* [Counter](middleware/counter.go) counts how many jobs start and finish.
* [Elapsed](middleware/elapsed.go) extends the counter middleware providing also:
  - the total amount of time.
  - the average time.
  - the time of the last executed job.
* [Wait](middleware/wait.go) allows to add a pause between worker jobs. (Job will
still be running concurrently if there are more workers) 

#### Job Wrapper
A job wrapper is a function that can transform and extend the job signature. 

Some common scenario that can benefit of job wrappers are jobs that
may fail and return an error. We could, for example, [retry the job](wrapper/retry.go) 
a certain amount of times.  

As an exercise let's log the job result with our favourite logging library using the 
["WithError" wrapper](wrapper/with_error.go);
```go
// jobLogger is a reusable logger wrapper for jobs.
jobLogger := func(jobName string) func(error) {
    return func(error) {
        if err != nil {
            logger.Error("job failed", "job", jobName, "error", err)
            return    
        }
        logger.Debug("job success", "job", jobName)
    }
}

job := function(ctx context.Context) error {
    err := someWorkThatCanFail()
    return err
}

pool := workers.Must(workers.New(
    wrapper.WithError(job, jobLogger("foo")
))
```

[ci-badge]: https://github.com/hmoragrega/workers/workflows/CI/badge.svg
[ci-url]:   https://github.com/hmoragrega/workers/actions?query=workflow%3ACI

[coverage-badge]: https://coveralls.io/repos/github/hmoragrega/workers/badge.svg?branch=main
[coverage-url]:   https://coveralls.io/github/hmoragrega/workers?branch=main

[godoc-badge]: https://pkg.go.dev/badge/github.com/hmoragrega/workers.svg
[godoc-url]:   https://pkg.go.dev/github.com/hmoragrega/workers

// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Pool runs a pool of workers over tasks pulled on demand from next.
//
// It is the engine under this package's parallel combinators, exported so
// that custom combinators can run concurrent work the same way: bounded by
// opts.Limit, subject to any cap installed by [WithMaxConcurrency], and with
// the same error handling.
//
// Each worker repeatedly pulls a task from next and passes it to task. Calls
// to next are serialized, so next needs no internal synchronization. It
// receives the pull index, which counts up from zero, and returns
// [ErrExhausted] when no more tasks are available. Any other error from
// next stops further pulling. task receives the same index alongside the
// pulled value, which is useful for attributing errors (see [IndexedError]).
// Pool does not wrap errors itself.
//
// opts.Limit is the number of workers, or [runtime.GOMAXPROCS] when it is
// less than or equal to zero: tasks are pulled on demand, so an unbounded
// pool has no meaning.
//
// By default, a task is pulled only when a worker is ready for it. With
// opts.Prefetch > 0, a dedicated goroutine pulls up to that many tasks ahead
// of the workers instead; see [ParallelOptions.Prefetch]. Either way, next
// is never called after Pool returns.
//
// If a global semaphore is present in ctx (via [WithMaxConcurrency]), each
// worker acquires it around task. If Pool is itself called from within a
// task holding a permit from that semaphore, it gives the permit back while
// the pool runs and reacquires it before returning, so nested pools cannot
// deadlock.
//
// By default, the first error cancels the remaining work, and that error is
// returned. With opts.JoinErrors, errors from task are recorded and the pool
// keeps going; an error from next still stops pulling, since the source is
// presumed broken. All recorded errors are joined in the result.
//
// Example:
//
//	// Upload files from a directory listing, 8 at a time.
//	err := flow.Pool(ctx,
//	    func(ctx context.Context, i int) (string, error) {
//	        if i >= len(paths) {
//	            return "", flow.ErrExhausted
//	        }
//	        return paths[i], nil
//	    },
//	    func(ctx context.Context, i int, path string) error {
//	        return upload(ctx, path)
//	    },
//	    flow.ParallelOptions{Limit: 8},
//	)
func Pool[U any](
	ctx context.Context,
	next func(ctx context.Context, i int) (U, error),
	task func(ctx context.Context, i int, u U) error,
	opts ParallelOptions,
) error {
	workers := opts.Limit
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	sem := getSemaphore(ctx)
	if sem != nil {
		defer yieldPermit(ctx, sem)()
	}
	group, subCtx := errgroup.WithContext(ctx)

	if opts.Prefetch > 0 {
		var stop func()
		next, stop = prefetch(subCtx, next, opts.Prefetch)
		defer stop()
	}

	var (
		mu   sync.Mutex
		idx  int
		done bool
		errs []error // JoinErrors mode
	)

	// record handles a worker error: in JoinErrors mode it is collected and
	// the worker keeps going; otherwise it propagates to the errgroup,
	// cancelling subCtx.
	record := func(err error) error {
		if !opts.JoinErrors {
			return err
		}
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
		return nil
	}

	// pull produces the next task under the mutex. ok is false when the pool
	// should stop pulling; err is non-nil for source or context errors, which
	// also stop the pool.
	pull := func() (i int, u U, ok bool, err error) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return 0, u, false, nil
		}
		if err := subCtx.Err(); err != nil {
			done = true
			return 0, u, false, err
		}
		u, err = next(subCtx, idx)
		if err != nil {
			done = true
			if errors.Is(err, ErrExhausted) {
				return 0, u, false, nil
			}
			return 0, u, false, err
		}
		i = idx
		idx++
		return i, u, true, nil
	}

	for range workers {
		group.Go(func() error {
			for {
				i, u, ok, err := pull()
				if err != nil {
					return record(err)
				}
				if !ok {
					return nil
				}

				taskCtx := subCtx
				var p *permit
				if sem != nil {
					if err := sem.Acquire(subCtx, 1); err != nil {
						return record(err)
					}
					taskCtx, p = withPermit(subCtx, sem)
				}
				err = task(taskCtx, i, u)
				if p != nil && p.held.Load() {
					sem.Release(1)
				}
				if err != nil {
					if e := record(err); e != nil {
						return e
					}
				}
			}
		})
	}

	err := group.Wait()
	if opts.JoinErrors {
		return errors.Join(errs...)
	}
	return err
}

// prefetch starts a goroutine that pulls from next ahead of demand, keeping
// at most n pulled items that pull has not yet returned.
//
// pull returns the results in order, or ctx's error if ctx is done first.
// It ignores its index argument: the goroutine passes next its own pull
// count, which matches the caller's as long as the caller counts its calls
// to pull from zero. The goroutine stops after the first error, including
// [ErrExhausted], and delivers it as the final result, so pull must not be
// called again after returning one. stop cancels the goroutine, aborting
// any pull in progress, and waits for it to exit, so next is never called
// after stop returns.
func prefetch[U any](
	ctx context.Context,
	next func(ctx context.Context, i int) (U, error),
	n int,
) (pull func(context.Context, int) (U, error), stop func()) {
	type result struct {
		u   U
		err error
	}
	// The goroutine holds one pulled item while blocked sending, so the
	// buffer holds the other n-1.
	results := make(chan result, n-1)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; ctx.Err() == nil; i++ {
			u, err := next(ctx, i)
			select {
			case results <- result{u: u, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	pull = func(ctx context.Context, _ int) (U, error) {
		select {
		case r := <-results:
			return r.u, r.err
		case <-ctx.Done():
			var zero U
			return zero, ctx.Err()
		}
	}
	stop = func() {
		cancel()
		<-done
	}
	return pull, stop
}

// parallelDo runs f(ctx, i) for i in [0, n) concurrently, respecting opts.
//
// If opts.Limit > 0, at most that many invocations run at once; otherwise
// all n may run simultaneously. opts.Prefetch is ignored, since the tasks
// are known up front. Error handling follows [Pool]: fail-fast by default,
// or collect-and-join with opts.JoinErrors.
func parallelDo(
	ctx context.Context,
	n int,
	opts ParallelOptions,
	f func(ctx context.Context, i int) error,
) error {
	if n == 0 {
		return nil
	}

	// Resolve "no limit" to one worker per task, and never start more
	// workers than tasks.
	if opts.Limit <= 0 || opts.Limit > n {
		opts.Limit = n
	}
	opts.Prefetch = 0

	return Pool(ctx,
		func(_ context.Context, i int) (int, error) {
			if i >= n {
				return 0, ErrExhausted
			}
			return i, nil
		},
		func(ctx context.Context, _ int, i int) error {
			return f(ctx, i)
		},
		opts,
	)
}

// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sync/errgroup"
)

// parallelPull runs a pool of workers over a pull-based task source.
//
// Each worker repeatedly calls next to obtain a task, then runs it. Calls to
// next are serialized by a mutex, so next needs no internal synchronization;
// it receives the pull index and returns [ErrExhausted] when no more tasks
// are available. Any other error from next stops further pulling.
//
// If a global semaphore is present in ctx (via [WithMaxConcurrency]), each
// worker acquires it around run.
//
// If opts.JoinErrors is true, errors from next and run are collected while
// the pool keeps going (though an error from next still stops pulling, since
// the source is presumed broken), and the joined result is returned. If false
// (the default), the first error cancels the remaining work (errgroup
// fail-fast). Note that opts.Limit is ignored here: concurrency is fixed by
// workers, which callers derive from opts.Limit as appropriate for their
// task shape.
func parallelPull[U any](
	ctx context.Context,
	workers int,
	opts ParallelOptions,
	next func(ctx context.Context, i int) (U, error),
	run func(ctx context.Context, i int, u U) error,
) error {
	sem := getSemaphore(ctx)
	group, subCtx := errgroup.WithContext(ctx)

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

				if sem != nil {
					if err := sem.Acquire(subCtx, 1); err != nil {
						return record(err)
					}
				}
				err = run(subCtx, i, u)
				if sem != nil {
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

// parallelDo runs f(ctx, i) for i in [0, n) concurrently, respecting opts.
//
// If opts.Limit > 0, at most that many invocations run at once; otherwise
// all n may run simultaneously. Error handling follows [parallelPull]:
// fail-fast by default, or collect-and-join with opts.JoinErrors.
func parallelDo(
	ctx context.Context,
	n int,
	opts ParallelOptions,
	f func(ctx context.Context, i int) error,
) error {
	if n == 0 {
		return nil
	}

	workers := n
	if opts.Limit > 0 && opts.Limit < n {
		workers = opts.Limit
	}

	return parallelPull(ctx, workers, opts,
		func(_ context.Context, i int) (int, error) {
			if i >= n {
				return 0, ErrExhausted
			}
			return i, nil
		},
		func(ctx context.Context, _ int, i int) error {
			return f(ctx, i)
		},
	)
}

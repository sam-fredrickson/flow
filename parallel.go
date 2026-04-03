// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"
)

// parallelDo runs f(ctx, i) for i in [0, n) concurrently, respecting opts.
//
// If opts.Limit > 0, at most that many goroutines run at once.
// If a global semaphore is present in ctx (via WithMaxConcurrency), each
// goroutine acquires it before calling f and releases it on return.
//
// If opts.JoinErrors is true, all invocations run to completion and their
// errors are joined. If false (the default), the first error cancels the
// remaining goroutines (errgroup fail-fast).
func parallelDo(
	ctx context.Context,
	n int,
	opts ParallelOptions,
	f func(ctx context.Context, i int) error,
) error {
	if n == 0 {
		return nil
	}

	sem := getSemaphore(ctx)

	group, subCtx := errgroup.WithContext(ctx)
	if opts.Limit > 0 {
		group.SetLimit(opts.Limit)
	}

	// JoinErrors mode: collect all errors, don't let errgroup cancel early.
	var errs chan error
	var joinedErr chan error
	if opts.JoinErrors {
		errs = make(chan error)
		joinedErr = make(chan error)
		go func() {
			var stepErrs []error
			for err := range errs {
				if err == nil {
					continue
				}
				stepErrs = append(stepErrs, err)
			}
			joinedErr <- errors.Join(stepErrs...)
		}()
	}

	var ctxErr error
	for i := range n {
		if err := ctx.Err(); err != nil {
			ctxErr = err
			break
		}
		group.Go(func() error {
			if err := subCtx.Err(); err != nil {
				if opts.JoinErrors {
					errs <- err
					return nil
				}
				return err
			}

			// Acquire global semaphore if present.
			if sem != nil {
				if err := sem.Acquire(subCtx, 1); err != nil {
					if opts.JoinErrors {
						errs <- err
						return nil
					}
					return err
				}
				defer sem.Release(1)
			}

			err := f(subCtx, i)
			if opts.JoinErrors {
				errs <- err
				return nil
			}
			return err
		})
	}

	err := group.Wait()
	if opts.JoinErrors {
		close(errs)
		err = <-joinedErr
		if ctxErr != nil {
			err = errors.Join(err, ctxErr)
		}
	} else if err == nil {
		err = ctxErr
	}
	return err
}

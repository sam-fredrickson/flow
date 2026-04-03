// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
)

// StepsProvider provides a sequence of steps.
//
// This allows workflows to grow dynamically at runtime; see [Steps], [Do]
// [InParallel], [From], and [Map].
type StepsProvider[T any] = Extract[T, []Step[T]]

// Options specifies how steps are run sequentially.
type Options struct {
	// JoinErrors controls error handling.
	//
	// By default, when false, the first step that returns an error stops
	// execution and that error is returned immediately.
	//
	// If enabled, all steps are run to completion regardless of step errors,
	// and a combined [errors.Join] error of all non-nil errors is returned.
	// Context cancellation still stops execution immediately; the context
	// error is included in the joined result.
	JoinErrors bool
}

// Do executes multiple steps in order, one at a time.
//
// This is the simplest way to run a sequence of static steps. For dynamic
// step sequences (StepsProvider), use [InSerial] instead.
//
// Do is the same as [DoWith] with the default [Options].
//
// Example:
//
//	Do(
//	    ValidateConfig(),
//	    StartDatabase(),
//	    StartServer(),
//	)
func Do[T any](steps ...Step[T]) Step[T] {
	return DoWith(Options{}, steps...)
}

// DoWith executes multiple steps in order, one at a time, with custom options.
//
// This allows configuring behavior like error joining. Use [Do] for the
// common case with default options.
//
// Example:
//
//	// Run all cleanup steps even if some fail, collect all errors
//	DoWith(
//	    Options{JoinErrors: true},
//	    CleanupTempFiles(),
//	    CloseConnections(),
//	    RemoveLockFiles(),
//	)
func DoWith[T any](opts Options, steps ...Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		var errs []error
		for _, step := range steps {
			if err := ctx.Err(); err != nil {
				if !opts.JoinErrors {
					return err
				}
				errs = append(errs, err)
				break
			}
			if err := step(ctx, t); err != nil {
				if !opts.JoinErrors {
					return err
				}
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}

// InSerial combines multiple step sequences and runs them in order, one
// at a time.
//
// InSerial is the same as [InSerialWith] with the default [Options].
//
// Example:
//
//	InSerial(
//	    flow.ForEach(GetFoos, ProcessFoo),
//	    flow.ForEach(GetBars, ProcessBar),
//	)
func InSerial[T any](providers ...StepsProvider[T]) Step[T] {
	return InSerialWith(Options{}, providers...)
}

// InSerialWith combines multiple step sequences and runs them in order, one
// at a time.
func InSerialWith[T any](
	opts Options,
	providers ...StepsProvider[T],
) Step[T] {
	return func(ctx context.Context, t T) error {
		var errs []error
		for _, provider := range providers {
			if err := ctx.Err(); err != nil {
				if !opts.JoinErrors {
					return err
				}
				errs = append(errs, err)
				break
			}
			steps, err := provider(ctx, t)
			if err != nil {
				if !opts.JoinErrors {
					return err
				}
				errs = append(errs, err)
				continue
			}
			for _, step := range steps {
				if err := ctx.Err(); err != nil {
					if !opts.JoinErrors {
						return err
					}
					errs = append(errs, err)
					break
				}
				if err := step(ctx, t); err != nil {
					if !opts.JoinErrors {
						return err
					}
					errs = append(errs, err)
				}
			}
		}
		return errors.Join(errs...)
	}
}

// ParallelOptions specifies how steps are run concurrently.
type ParallelOptions struct {
	// Limit controls how many goroutines may run.
	//
	// Numbers less than or equal to zero indicate no limit.
	Limit int

	// JoinErrors controls error handling.
	//
	// By default, when false, the first step that returns an error cancels
	// the rest, and this first error is returned. (This is the behavior of
	// the `errgroup` package.)
	//
	// If enabled, all steps are run to completion regardless of step errors,
	// and a combined [errors.Join] error of all non-nil errors is returned.
	// Context cancellation still stops execution immediately; no new
	// goroutines are scheduled and the context error is included in the
	// joined result.
	JoinErrors bool
}

// InParallel combines multiple step sequences and runs them concurrently.
//
// InParallel is the same as [InParallelWith] with the default [ParallelOptions].
func InParallel[T any](providers ...StepsProvider[T]) Step[T] {
	return InParallelWith(ParallelOptions{}, providers...)
}

// InParallelWith combines multiple step sequences and runs them concurrently.
//
// All step sequences are expanded sequentially first, then the resulting steps
// are run in separate goroutines.
func InParallelWith[T any](
	opts ParallelOptions,
	providers ...StepsProvider[T],
) Step[T] {
	return func(ctx context.Context, t T) error {
		// expand all providers sequentially to get all steps
		var allSteps []Step[T]
		for _, next := range providers {
			if err := ctx.Err(); err != nil {
				return err
			}
			steps, err := next(ctx, t)
			if err != nil {
				return err
			}
			allSteps = append(allSteps, steps...)
		}

		return parallelDo(ctx, len(allSteps), opts, func(ctx context.Context, i int) error {
			return allSteps[i](ctx, t)
		})
	}
}

// Steps creates a static step sequence from the provided steps.
//
// This is useful when you need to convert static steps into a [StepsProvider]
// for use with [InSerial] or [InParallel]. For simple static sequences,
// prefer using [Do] directly.
//
// Example:
//
//	// Prefer Do for simple static sequences
//	Do(ValidateConfig(), StartDatabase(), RunMigrations())
//
//	// Use Steps when you need a StepsProvider
//	InSerial(
//	    Steps(ValidateConfig(), StartDatabase()),
//	    flow.ForEach(GetServices, DeployService),
//	)
func Steps[T any](steps ...Step[T]) StepsProvider[T] {
	return func(ctx context.Context, t T) ([]Step[T], error) {
		return steps, nil
	}
}

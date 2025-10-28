// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"time"
)

// WithTimeout wraps a step with a timeout.
//
// The step is executed with a derived context that will be cancelled after
// the specified duration. If the step does not complete within the timeout,
// it will receive a cancelled context and should return [context.DeadlineExceeded].
//
// Example:
//
//	flow.WithTimeout(5*time.Second, ExpensiveOperation())
func WithTimeout[T any](timeout time.Duration, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return step(ctx, t)
	}
}

// WithDeadline wraps a step with an absolute deadline.
//
// The step is executed with a derived context that will be cancelled at the
// specified time. If the step does not complete before the deadline, it will
// receive a cancelled context and should return [context.DeadlineExceeded].
//
// Example:
//
//	deadline := time.Now().Add(5 * time.Second)
//	flow.WithDeadline(deadline, ExpensiveOperation())
func WithDeadline[T any](deadline time.Time, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		ctx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		return step(ctx, t)
	}
}

// Sleep pauses execution for the specified duration.
//
// The sleep respects context cancellation, returning [context.Canceled] if
// the context is cancelled before the duration elapses.
//
// This is useful in polling loops or when adding delays between operations.
//
// Example:
//
//	flow.While(
//	    ServiceNotReady(),
//	    flow.Do(
//	        CheckStatus(),
//	        flow.Sleep(5*time.Second),
//	    ),
//	)
func Sleep[T any](duration time.Duration) Step[T] {
	return func(ctx context.Context, t T) error {
		select {
		case <-time.After(duration):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

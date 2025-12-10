// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"log"
	"log/slog"
	"time"
)

// flowCtxKey is the context key for retrieving the flowCtx.
type flowCtxKey struct{}

// flowCtx is an internal context type that consolidates all flow-specific
// context values into a single lookup.
//
// Instead of storing trace, names, and loggers as separate context values
// (each requiring O(n) chain traversal), flowCtx stores all of them together
// and requires only a single O(1) lookup via flowCtxKey.
//
// The flowCtx embeds the parent context.Context to properly delegate
// cancellation, deadlines, and non-flow context values.
type flowCtx struct {
	context.Context

	// trace is the active execution trace, if tracing is enabled.
	// nil if no trace is active.
	trace *trace

	// names is the step name stack in hierarchical order (oldest first).
	// nil represents an empty name stack.
	names []string

	// logger is the active log.Logger for the workflow.
	// Defaults to log.Default() if not explicitly set.
	logger *log.Logger

	// slogger is the active slog.Logger for structured logging.
	// Defaults to slog.Default() if not explicitly set.
	slogger *slog.Logger
}

// Value implements context.Context.Value by intercepting flowCtxKey lookups
// and delegating all other keys to the embedded parent context.
//
// This allows flowCtx to act as a normal context.Context while providing
// efficient access to flow-specific values.
func (f *flowCtx) Value(key any) any {
	_, ok := key.(flowCtxKey)
	if !ok {
		return f.Context.Value(key)
	}
	return f
}

// newFlowCtx creates a new flowCtx that wraps parent and inherits flow-specific
// state from origin.
//
// If origin is nil, creates a fresh flowCtx with default values:
//   - trace: nil (no tracing)
//   - names: nil (no name stack)
//   - logger: log.Default()
//   - slogger: slog.Default()
func newFlowCtx(parent context.Context, origin *flowCtx) *flowCtx {
	if origin == nil {
		origin = &flowCtx{
			Context: parent,
			trace:   nil,
			names:   nil,
			logger:  log.Default(),
			slogger: slog.Default(),
		}
	}
	f := &flowCtx{
		Context: parent,
		trace:   origin.trace,
		names:   origin.names,
		logger:  origin.logger,
		slogger: origin.slogger,
	}
	return f
}

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
		timer := time.NewTimer(duration)
		defer timer.Stop()

		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

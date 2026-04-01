// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNoScope is returned by [Manage] when no enclosing [Scope] is active.
// This indicates a programmer error — the step must be wrapped in a [Scope].
var ErrNoScope = errors.New("no enclosing Scope — wrap the step with flow.Scope")

// cleanupScope holds cleanup functions registered by Manage within a Scope.
type cleanupScope struct {
	mu       sync.Mutex
	cleanups []func(context.Context) error
}

// register appends a cleanup function. Safe for concurrent calls.
func (s *cleanupScope) register(fn func(context.Context) error) {
	s.mu.Lock()
	s.cleanups = append(s.cleanups, fn)
	s.mu.Unlock()
}

// runAll executes all registered cleanups in LIFO order, joining errors.
// Must only be called after all goroutines that might register cleanups have joined.
func (s *cleanupScope) runAll(ctx context.Context) error {
	var errs []error
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		if err := s.cleanups[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// getScope retrieves the active cleanupScope from context.
// Returns nil if no scope is active.
func getScope(ctx context.Context) *cleanupScope {
	f, _ := ctx.Value(flowCtxKey{}).(*flowCtx)
	if f == nil {
		return nil
	}
	return f.scope
}

// Manage pairs an acquire [Step] with a cleanup [Step], registering the cleanup
// with the enclosing [Scope].
//
// This is the primary API for scope-based resource cleanup. The acquire step
// typically stores the resource in state T; the cleanup step reads it back
// from state T. No intermediate wrapper type is needed.
//
// If the acquire step fails, no cleanup is registered (nothing to clean up).
// Cleanup runs in LIFO order when the enclosing [Scope] exits.
//
// Manage returns [ErrNoScope] if no enclosing [Scope] is active.
//
// Manage requires a pointer type for state (enforced at compile time).
// The cleanup closure captures the pointer at registration time; a value type
// would silently operate on a copy, missing any mutations made after
// registration.
//
// Example:
//
//	flow.Scope(
//	    flow.Do(
//	        flow.Manage(openAndStoreConn, closeConn),
//	        flow.Manage(beginAndStoreTx, rollbackTx),
//	        doWork,
//	    ),
//	)
func Manage[T any, P interface{ *T }](acquire Step[P], cleanup Step[P]) Step[P] {
	return func(ctx context.Context, t P) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := acquire(ctx, t); err != nil {
			return err
		}
		scope := getScope(ctx)
		if scope == nil {
			return ErrNoScope
		}
		scope.register(func(cleanCtx context.Context) error {
			if err := cleanCtx.Err(); err != nil {
				return err
			}
			return cleanup(cleanCtx, t)
		})
		return nil
	}
}

// Scope wraps a [Step] with resource cleanup management.
//
// Resources registered via [Manage] within the step are automatically cleaned up
// in LIFO (last-in, first-out) order when the scope exits. Cleanup errors are
// joined with the step error via [errors.Join].
//
// If [WithCleanupTimeout] is active, cleanup runs with a detached context and
// independent timeout, ensuring cleanup can proceed even if the step's context
// was cancelled.
func Scope[T any](step Step[T]) Step[T] {
	return func(ctx context.Context, t T) (returnErr error) {
		if err := ctx.Err(); err != nil {
			return err
		}

		f, _ := ctx.Value(flowCtxKey{}).(*flowCtx)
		f2 := newFlowCtx(ctx, f)
		scope := &cleanupScope{}
		f2.scope = scope

		defer func() {
			cleanupCtx := context.Context(f2)
			if f2.cleanupTimeout > 0 {
				detached := context.WithoutCancel(f2)
				var cancel context.CancelFunc
				cleanupCtx, cancel = context.WithTimeout(detached, f2.cleanupTimeout)
				defer cancel()
			}

			cleanupErr := scope.runAll(cleanupCtx)
			returnErr = errors.Join(returnErr, cleanupErr)
		}()

		return step(f2, t)
	}
}

// WithCleanupTimeout configures the cleanup timeout for [Scope] steps within
// the wrapped step's subtree.
//
// When a [Scope] exits, if a cleanup timeout is configured, the cleanup context
// is detached from parent cancellation (via [context.WithoutCancel]) and given
// an independent timeout. This ensures cleanup can proceed even if the step's
// context was cancelled.
//
// The timeout is inherited through nested scopes and can be overridden by a
// closer WithCleanupTimeout call.
func WithCleanupTimeout[T any](timeout time.Duration, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		f, _ := ctx.Value(flowCtxKey{}).(*flowCtx)
		f2 := newFlowCtx(ctx, f)
		f2.cleanupTimeout = timeout
		return step(f2, t)
	}
}

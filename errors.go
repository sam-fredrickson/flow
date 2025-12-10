// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"fmt"
)

// IgnoreError wraps a step to always return nil, even if the step fails.
//
// This is useful for "best effort" operations where failures should not stop
// the overall flow.
func IgnoreError[T any](step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		_ = step(ctx, t)
		return nil
	}
}

// RecoveredPanic is an error type that wraps a panic value.
type RecoveredPanic struct {
	Value any
}

func (p *RecoveredPanic) Error() string {
	return fmt.Sprintf("panic recovered: %v", p.Value)
}

// RecoverPanics wraps a step to recover from panics and convert them to errors.
//
// If the step panics, the panic value is wrapped in a [RecoveredPanic] error.
// This is useful for defensive programming when calling code that may panic.
func RecoverPanics[T any](step Step[T]) Step[T] {
	return func(ctx context.Context, t T) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &RecoveredPanic{Value: r}
			}
		}()
		return step(ctx, t)
	}
}

// OnError provides dynamic error handling with fallback steps.
//
// If the primary step fails, onError is called with the error and can return
// a fallback step to execute instead. The onError transform has access to the
// context, state, and the error, allowing for sophisticated error handling
// strategies.
//
// If onError itself returns an error, that error is propagated and no fallback
// step is executed. If onError returns (nil, nil), the error is considered
// handled and OnError returns nil (success). This allows selectively swallowing
// certain errors while propagating others.
//
// Example:
//
//	OnError(
//	    CallPrimaryAPI,
//	    func(ctx context.Context, state *State, err error) (Step[*State], error) {
//	        if errors.Is(err, ErrTimeout) {
//	            return CallBackupAPI, nil
//	        }
//	        if errors.Is(err, ErrRateLimit) {
//	            return RetryAfterDelay, nil
//	        }
//	        if errors.Is(err, ErrNotFound) {
//	            return nil, nil // Not found is acceptable, treat as success
//	        }
//	        return nil, fmt.Errorf("unrecoverable: %w", err)
//	    },
//	)
func OnError[T any](step Step[T], onError Transform[T, error, Step[T]]) Step[T] {
	return func(ctx context.Context, t T) error {
		if err := step(ctx, t); err != nil {
			fallback, err := onError(ctx, t, err)
			if err != nil {
				return err
			}
			if fallback == nil {
				return nil
			}
			return fallback(ctx, t)
		}
		return nil
	}
}

// FallbackTo returns an error transform that always returns the provided
// fallback step.
//
// This is useful with [OnError] when you unconditionally want to run a fallback
// step on error, regardless of the state or the original error.
func FallbackTo[T any](fallback Step[T]) Transform[T, error, Step[T]] {
	return func(_ context.Context, _ T, _ error) (Step[T], error) {
		return fallback, nil
	}
}

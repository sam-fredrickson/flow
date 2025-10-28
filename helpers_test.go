// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// ==== Test Helpers: Test Runner ====

// runStepTest is a helper that runs a test case with a Step[*CountingFlow],
// validating both the error and the final counter value.
func runStepTest(t *testing.T, step Step[*CountingFlow], expectedCounter int64, validator func(error) error) {
	t.Helper()
	var c CountingFlow
	testErr := step(t.Context(), &c)
	if err := validator(testErr); err != nil {
		t.Error(err)
	}
	if c.Counter != expectedCounter {
		t.Errorf("got counter %d, want %d", c.Counter, expectedCounter)
	}
}

// ==== Test Helpers: Error Variables ====

var error1 = errors.New("error 1")
var error2 = errors.New("error 2")
var error3 = errors.New("error 3")
var errorRetryable = errors.New("retryable error")
var errorNonRetryable = errors.New("non-retryable error")

// ==== Test Helpers: CountingFlow ====

type CountingFlow struct {
	Counter int64
}

type ChildState struct {
	Value int64
}

// Increment atomically adds n to the counter.
func Increment(n int64) Step[*CountingFlow] {
	return func(ctx context.Context, c *CountingFlow) error {
		atomic.AddInt64(&c.Counter, n)
		return nil
	}
}

// Decrement atomically subtracts n from the counter.
func Decrement(n int64) Step[*CountingFlow] {
	return Increment(-n)
}

// GetCount returns the current counter value.
func GetCount(ctx context.Context, c *CountingFlow) (int64, error) {
	return atomic.LoadInt64(&c.Counter), nil
}

// GetCountFailing always returns error1, used to test extract error handling.
func GetCountFailing(ctx context.Context, c *CountingFlow) (int64, error) {
	return 0, error1
}

// SendCount is a no-op consume function for testing pipelines.
func SendCount(ctx context.Context, flow *CountingFlow, n int64) error {
	return nil
}

// CountEquals returns a predicate that checks if the counter equals the given value.
func CountEquals(value int64) Predicate[*CountingFlow] {
	return func(_ context.Context, c *CountingFlow) (bool, error) {
		return c.Counter == value, nil
	}
}

// CountGreaterThan returns a predicate that checks if the counter is greater than the given value.
func CountGreaterThan(value int64) Predicate[*CountingFlow] {
	return func(_ context.Context, c *CountingFlow) (bool, error) {
		return c.Counter > value, nil
	}
}

// FailingPredicate returns a predicate that always fails with the given error.
func FailingPredicate(err error) Predicate[*CountingFlow] {
	return func(_ context.Context, _ *CountingFlow) (bool, error) {
		return false, err
	}
}

// FailUntilCount increments the counter on each call and returns an error
// until the counter reaches the threshold value.
func FailUntilCount(threshold int64) Step[*CountingFlow] {
	return func(_ context.Context, c *CountingFlow) error {
		current := atomic.AddInt64(&c.Counter, 1)
		if current < threshold {
			return errors.New("not ready yet")
		}
		return nil
	}
}

// IncrementAndFail increments the counter by 1 and then returns the given error.
func IncrementAndFail(err error) Step[*CountingFlow] {
	return func(_ context.Context, c *CountingFlow) error {
		atomic.AddInt64(&c.Counter, 1)
		return err
	}
}

// PanicWith triggers a panic with the given value, used to test panic recovery.
func PanicWith(value any) Step[*CountingFlow] {
	return func(_ context.Context, _ *CountingFlow) error {
		panic(value)
	}
}

// ==== Test Helpers: FailingFlow ====

type FailingFlow struct {
}

// DoSomething returns a step that returns the given error (or nil).
func DoSomething(err error) Step[FailingFlow] {
	return func(_ context.Context, _ FailingFlow) error {
		return err
	}
}

// ==== Test Helpers: Error Validators ====

// isNil validates that the error is nil.
func isNil(testErr error) error {
	if testErr != nil {
		return fmt.Errorf("unexpected error: %w", testErr)
	}
	return nil
}

// isNotNil validates that the error is not nil.
func isNotNil(testErr error) error {
	if testErr == nil {
		return fmt.Errorf("expected error but got nil")
	}
	return nil
}

// all returns a validator that passes only if all the given validators pass.
func all(validators ...func(error) error) func(error) error {
	return func(testErr error) error {
		for _, validator := range validators {
			if err := validator(testErr); err != nil {
				return err
			}
		}
		return nil
	}
}

// oneOf returns a validator that passes only if exactly one of the given validators passes.
func oneOf(validators ...func(error) error) func(error) error {
	return func(testErr error) error {
		count := 0
		for _, validator := range validators {
			if err := validator(testErr); err == nil {
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("expected 1 validator, got %d", count)
		}
		return nil
	}
}

// matches returns a validator that checks if the error matches the target error using errors.Is.
func matches(targetErr error) func(error) error {
	return func(testError error) error {
		m := errors.Is(testError, targetErr)
		if !m {
			return fmt.Errorf("expected error %v to match error %v", testError, targetErr)
		}

		return nil
	}
}

// notMatches returns a validator that checks if the error does not match the target error.
func notMatches(targetErr error) func(error) error {
	return func(testError error) error {
		m := errors.Is(testError, targetErr)
		if m {
			return fmt.Errorf("expected error %v to not match error %v", testError, targetErr)
		}
		return nil
	}
}

// isRecoveredPanic validates that the error is a RecoveredPanic.
func isRecoveredPanic(testErr error) error {
	var recoveredPanic *RecoveredPanic
	if !errors.As(testErr, &recoveredPanic) {
		return fmt.Errorf("expected RecoveredPanic error, got %v", testErr)
	}
	return nil
}

// contains returns a validator that checks if the error message contains the given substring.
func contains(substring string) func(error) error {
	return func(testErr error) error {
		if !strings.Contains(testErr.Error(), substring) {
			return fmt.Errorf("expected error to contain %q, got %q", substring, testErr)
		}
		return nil
	}
}

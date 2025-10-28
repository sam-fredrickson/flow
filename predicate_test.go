// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestConditionals(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		step     Step[*CountingFlow]
		expected int64
	}{
		{
			name: "WhenTrue",
			step: When(
				CountEquals(0),
				Increment(5),
			),
			expected: 5,
		},
		{
			name: "WhenFalse",
			step: Do(
				Increment(1),
				When(
					CountEquals(0),
					Increment(5),
				),
			),
			expected: 1, // predicate is false, step skipped
		},
		{
			name: "UnlessTrue",
			step: Unless(
				CountEquals(0),
				Increment(5),
			),
			expected: 0, // predicate is true, step skipped
		},
		{
			name: "UnlessFalse",
			step: Do(
				Increment(1),
				Unless(
					CountEquals(0),
					Increment(5),
				),
			),
			expected: 6, // predicate is false, step runs
		},
		{
			name: "WhenInSerial",
			step: Do(
				Increment(1),
				When(
					CountGreaterThan(0),
					Increment(2),
				),
				When(
					CountGreaterThan(10),
					Increment(100),
				),
			),
			expected: 3, // 1 + 2, but not 100 (counter is 3, not > 10)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c CountingFlow
			_ = tc.step(t.Context(), &c)
			if c.Counter != tc.expected {
				t.Errorf("got %d, want %d", c.Counter, tc.expected)
			}
		})
	}
}

func TestPredicateErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		step      Step[*CountingFlow]
		validator func(error) error
	}{
		{
			name: "WhenPredicateError",
			step: When(
				FailingPredicate(error1),
				Increment(5),
			),
			validator: matches(error1),
		},
		{
			name: "UnlessPredicateError",
			step: Unless(
				FailingPredicate(error1),
				Increment(5),
			),
			validator: matches(error1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c CountingFlow
			testErr := tc.step(t.Context(), &c)
			if err := tc.validator(testErr); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestWhile(t *testing.T) {
	t.Parallel()
	t.Run("ExecutesUntilPredicateFalse", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// Loop while counter < 5, incrementing by 1 each time
		step := While(
			func(_ context.Context, cf *CountingFlow) (bool, error) {
				return cf.Counter < 5, nil
			},
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 5 {
			t.Errorf("expected counter=5, got %d", c.Counter)
		}
	})

	t.Run("NoIterationsIfPredicateFalse", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// Predicate is false from the start
		step := While(
			func(_ context.Context, cf *CountingFlow) (bool, error) {
				return false, nil
			},
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 0 {
			t.Errorf("expected counter=0, got %d", c.Counter)
		}
	})

	t.Run("PropagatesStepError", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		expectedErr := errors.New("step error")
		step := While(
			func(_ context.Context, cf *CountingFlow) (bool, error) {
				return cf.Counter < 10, nil
			},
			func(_ context.Context, cf *CountingFlow) error {
				atomic.AddInt64(&cf.Counter, 1)
				if cf.Counter >= 3 {
					return expectedErr
				}
				return nil
			},
		)
		err := step(t.Context(), &c)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
		if c.Counter != 3 {
			t.Errorf("expected counter=3 (failed on third iteration), got %d", c.Counter)
		}
	})

	t.Run("PropagatesPredicateError", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		expectedErr := errors.New("predicate error")
		step := While(
			func(_ context.Context, cf *CountingFlow) (bool, error) {
				if cf.Counter >= 3 {
					return false, expectedErr
				}
				return true, nil
			},
			Increment(1),
		)
		err := step(t.Context(), &c)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
		if c.Counter != 3 {
			t.Errorf("expected counter=3, got %d", c.Counter)
		}
	})

	t.Run("WithTimeout", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// Loop that would run forever, but timeout after 100ms
		step := WithTimeout(
			100*time.Millisecond,
			While(
				func(_ context.Context, cf *CountingFlow) (bool, error) {
					return true, nil // Always true - would loop forever
				},
				Do(
					Increment(1),
					Sleep[*CountingFlow](20*time.Millisecond),
				),
			),
		)
		err := step(t.Context(), &c)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
		// Should have executed a few times before timeout
		if c.Counter < 1 {
			t.Errorf("expected at least 1 iteration, got %d", c.Counter)
		}
	})
}

func TestNot(t *testing.T) {
	t.Parallel()
	t.Run("NegatesTrue", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		step := When(
			Not(func(_ context.Context, _ *CountingFlow) (bool, error) {
				return true, nil
			}),
			Increment(5),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 0 {
			t.Errorf("expected counter=0 (step skipped), got %d", c.Counter)
		}
	})

	t.Run("NegatesFalse", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		step := When(
			Not(func(_ context.Context, _ *CountingFlow) (bool, error) {
				return false, nil
			}),
			Increment(5),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 5 {
			t.Errorf("expected counter=5 (step executed), got %d", c.Counter)
		}
	})

	t.Run("PropagatesError", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		expectedErr := errors.New("predicate error")
		step := When(
			Not(func(_ context.Context, _ *CountingFlow) (bool, error) {
				return false, expectedErr
			}),
			Increment(5),
		)
		err := step(t.Context(), &c)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
	})

	t.Run("WithWhile", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// While Not(condition) - loop until counter >= 5
		step := While(
			Not(func(_ context.Context, cf *CountingFlow) (bool, error) {
				return cf.Counter >= 5, nil
			}),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 5 {
			t.Errorf("expected counter=5, got %d", c.Counter)
		}
	})
}

func TestAnd(t *testing.T) {
	t.Parallel()
	t.Run("AllTrue", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		c.Counter = 10
		step := When(
			And(
				CountGreaterThan(5),
				CountGreaterThan(8),
				CountGreaterThan(9),
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 11 {
			t.Errorf("expected counter=11 (step executed), got %d", c.Counter)
		}
	})

	t.Run("OneFalse", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		c.Counter = 10
		step := When(
			And(
				CountGreaterThan(5),
				CountGreaterThan(15), // This is false
				CountGreaterThan(9),
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 10 {
			t.Errorf("expected counter=10 (step skipped), got %d", c.Counter)
		}
	})

	t.Run("ShortCircuits", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		callCount := int64(0)
		step := When(
			And(
				func(_ context.Context, _ *CountingFlow) (bool, error) {
					atomic.AddInt64(&callCount, 1)
					return false, nil // First predicate is false
				},
				func(_ context.Context, _ *CountingFlow) (bool, error) {
					atomic.AddInt64(&callCount, 1)
					return true, nil // Should not be called
				},
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount != 1 {
			t.Errorf("expected 1 predicate call (short-circuit), got %d", callCount)
		}
	})

	t.Run("PropagatesError", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		expectedErr := errors.New("predicate error")
		step := When(
			And(
				CountEquals(0), // This is true, so second predicate gets evaluated
				func(_ context.Context, _ *CountingFlow) (bool, error) {
					return false, expectedErr
				},
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
	})
}

func TestOr(t *testing.T) {
	t.Parallel()
	t.Run("AllFalse", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		c.Counter = 5
		step := When(
			Or(
				CountGreaterThan(10),
				CountGreaterThan(8),
				CountGreaterThan(6),
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 5 {
			t.Errorf("expected counter=5 (step skipped), got %d", c.Counter)
		}
	})

	t.Run("OneTrue", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		c.Counter = 5
		step := When(
			Or(
				CountGreaterThan(10),
				CountGreaterThan(3), // This is true
				CountGreaterThan(6),
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 6 {
			t.Errorf("expected counter=6 (step executed), got %d", c.Counter)
		}
	})

	t.Run("ShortCircuits", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		callCount := int64(0)
		step := When(
			Or(
				func(_ context.Context, _ *CountingFlow) (bool, error) {
					atomic.AddInt64(&callCount, 1)
					return true, nil // First predicate is true
				},
				func(_ context.Context, _ *CountingFlow) (bool, error) {
					atomic.AddInt64(&callCount, 1)
					return true, nil // Should not be called
				},
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount != 1 {
			t.Errorf("expected 1 predicate call (short-circuit), got %d", callCount)
		}
	})

	t.Run("PropagatesError", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		expectedErr := errors.New("predicate error")
		step := When(
			Or(
				CountGreaterThan(10),
				func(_ context.Context, _ *CountingFlow) (bool, error) {
					return false, expectedErr
				},
			),
			Increment(1),
		)
		err := step(t.Context(), &c)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
	})
}

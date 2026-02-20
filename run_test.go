// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestSmoke(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		step     Step[*CountingFlow]
		expected int64
	}{
		{
			name:     "Single",
			step:     Increment(1),
			expected: 1,
		},
		{
			name: "Serial",
			step: Do(
				Increment(1), Increment(2),
			),
			expected: 3,
		},
		{
			name: "Parallel",
			step: InParallel(
				Steps(
					Increment(15),
					Decrement(5),
					Increment(21),
					Decrement(10),
					Decrement(11),
				),
			),
			expected: 10,
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

func TestStepsProviderErrors(t *testing.T) {
	t.Parallel()
	errorSteps := errors.New("steps expansion error")

	failingProvider :=
		func(_ context.Context, _ *CountingFlow) (
			[]Step[*CountingFlow], error,
		) {
			return nil, errorSteps
		}

	testCases := []struct {
		name      string
		step      Step[*CountingFlow]
		validator func(error) error
	}{
		{
			name: "InSerialError",
			step: InSerial(
				Steps(Increment(1)),
				failingProvider,
				Steps(Increment(2)),
			),
			validator: matches(errorSteps),
		},
		{
			name: "InSerialWithError",
			step: InSerialWith(
				Options{JoinErrors: true},
				Steps(Increment(1)),
				failingProvider,
				Steps(Increment(2)),
			),
			validator: matches(errorSteps),
		},
		{
			name: "InSerialWithStepErrors",
			step: InSerialWith(
				Options{JoinErrors: true},
				Steps(
					Increment(1),
					IncrementAndFail(error1),
					IncrementAndFail(error2),
				),
			),
			validator: all(
				matches(error1),
				matches(error2),
			),
		},
		{
			name: "InParallelError",
			step: InParallel(
				Steps(Increment(1)),
				failingProvider,
			),
			validator: matches(errorSteps),
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

func TestJoinErrorsRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	// cancelAndIncrement returns a step that increments the counter and
	// cancels the context. Critically, it does NOT return an error and does
	// NOT check ctx.Err() — so the only thing that can stop subsequent
	// steps is the loop itself.
	cancelAndIncrement := func(cancel context.CancelFunc) Step[*CountingFlow] {
		return func(_ context.Context, c *CountingFlow) error {
			atomic.AddInt64(&c.Counter, 1)
			cancel()
			return nil
		}
	}

	// cancellingProvider increments the counter and cancels the context
	// during provider expansion (before any steps run).
	cancellingProvider := func(cancel context.CancelFunc) StepsProvider[*CountingFlow] {
		return func(_ context.Context, c *CountingFlow) ([]Step[*CountingFlow], error) {
			atomic.AddInt64(&c.Counter, 1)
			cancel()
			return nil, nil
		}
	}

	// countingProvider increments the counter during expansion. Used to
	// detect whether a provider was expanded when it shouldn't have been.
	countingProvider := func(_ context.Context, c *CountingFlow) ([]Step[*CountingFlow], error) {
		atomic.AddInt64(&c.Counter, 1)
		return nil, nil
	}

	t.Run("DoWith stops steps after cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var c CountingFlow
		step := DoWith(Options{JoinErrors: true},
			cancelAndIncrement(cancel),
			Increment(1),
			Increment(1),
		)

		err := step(ctx, &c)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if c.Counter != 1 {
			t.Errorf("expected counter 1, got %d", c.Counter)
		}
	})

	t.Run("InSerialWith stops steps after cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var c CountingFlow
		step := InSerialWith(Options{JoinErrors: true},
			Steps(
				cancelAndIncrement(cancel),
				Increment(1),
				Increment(1),
			),
		)

		err := step(ctx, &c)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if c.Counter != 1 {
			t.Errorf("expected counter 1, got %d", c.Counter)
		}
	})

	t.Run("InSerialWith stops providers after cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var c CountingFlow
		step := InSerialWith(Options{JoinErrors: true},
			cancellingProvider(cancel),
			countingProvider,
		)

		err := step(ctx, &c)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if c.Counter != 1 {
			t.Errorf("expected counter 1, got %d", c.Counter)
		}
	})

	t.Run("InParallelWith stops providers after cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var c CountingFlow
		step := InParallelWith(ParallelOptions{JoinErrors: true},
			cancellingProvider(cancel),
			countingProvider,
		)

		err := step(ctx, &c)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if c.Counter != 1 {
			t.Errorf("expected counter 1, got %d", c.Counter)
		}
	})

	t.Run("InParallelWith stops scheduling after cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var c CountingFlow
		step := InParallelWith(
			ParallelOptions{JoinErrors: true, Limit: 1},
			Steps(
				cancelAndIncrement(cancel),
				Increment(1),
				Increment(1),
			),
		)

		err := step(ctx, &c)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if c.Counter != 1 {
			t.Errorf("expected counter 1, got %d", c.Counter)
		}
	})
}

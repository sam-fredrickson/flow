// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
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

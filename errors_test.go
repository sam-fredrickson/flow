// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"testing"
)

func TestErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		step      Step[FailingFlow]
		validator func(error) error
	}{
		{
			name:      "SingleNonNil",
			step:      DoSomething(nil),
			validator: isNil,
		},
		{
			name:      "SingleError",
			step:      DoSomething(error1),
			validator: matches(error1),
		},
		{
			name: "SerialFirstError",
			step: Do(
				DoSomething(nil),
				DoSomething(error1),
				DoSomething(error2),
			),
			validator: all(
				matches(error1),
				notMatches(error2),
			),
		},
		{
			name: "SerialJoinErrors",
			step: DoWith(
				Options{
					JoinErrors: true,
				},
				DoSomething(nil),
				DoSomething(error1),
				DoSomething(error2),
			),
			validator: all(
				matches(error1),
				matches(error2),
			),
		},
		{
			name: "ParallelFirstError",
			step: InParallel(
				Steps(
					DoSomething(nil),
					DoSomething(error1),
					DoSomething(error2),
				),
			),
			validator: oneOf(
				matches(error1),
				matches(error2),
			),
		},
		{
			name: "ParallelJoinErrors",
			step: InParallelWith(
				ParallelOptions{
					Limit:      3,
					JoinErrors: true,
				},
				Steps(
					DoSomething(nil),
					DoSomething(error1),
					DoSomething(error2),
				),
			),
			validator: all(
				matches(error1),
				matches(error2),
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testErr := tc.step(t.Context(), FailingFlow{})
			if err := tc.validator(testErr); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestIgnoreError(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
	}{
		{
			name: "IgnoresError",
			step: Do(
				IgnoreError(IncrementAndFail(error1)),
				Increment(5),
			),
			// 1 from failed step + 5 from success
			expectedCounter: 6,
		},
		{
			name: "IgnoresSuccess",
			step: Do(
				IgnoreError(Increment(3)),
				Increment(2),
			),
			// both succeed
			expectedCounter: 5,
		},
		{
			name: "MultipleIgnored",
			step: Do(
				IgnoreError(IncrementAndFail(error1)),
				IgnoreError(IncrementAndFail(error2)),
				Increment(1),
			),
			// all three run despite errors
			expectedCounter: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c CountingFlow
			err := tc.step(t.Context(), &c)
			if err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
			if c.Counter != tc.expectedCounter {
				t.Errorf("got counter %d, want %d", c.Counter, tc.expectedCounter)
			}
		})
	}
}

func TestOnError(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "NoError",
			step: OnError(
				Increment(5),
				func(_ context.Context, _ *CountingFlow, err error) (Step[*CountingFlow], error) {
					return Increment(100), nil
				},
			),
			expectedCounter: 5,
			validator:       isNil,
		},
		{
			name: "FallbackExecuted",
			step: OnError(
				IncrementAndFail(error1),
				func(_ context.Context, _ *CountingFlow, err error) (Step[*CountingFlow], error) {
					if errors.Is(err, error1) {
						return Increment(10), nil
					}
					return Increment(20), nil
				},
			),
			expectedCounter: 11, // 1 from fail + 10 from fallback
			validator:       isNil,
		},
		{
			name: "FallbackError",
			step: OnError(
				IncrementAndFail(error1),
				func(_ context.Context, _ *CountingFlow, err error) (Step[*CountingFlow], error) {
					return nil, error2
				},
			),
			expectedCounter: 1,
			validator:       matches(error2),
		},
		{
			name: "FallbackStepFails",
			step: OnError(
				IncrementAndFail(error1),
				func(_ context.Context, _ *CountingFlow, err error) (Step[*CountingFlow], error) {
					return IncrementAndFail(error3), nil
				},
			),
			expectedCounter: 2, // 1 from initial + 1 from fallback
			validator:       matches(error3),
		},
		{
			name: "FallbackToNoError",
			step: OnError(
				Increment(5),
				FallbackTo(Increment(100)),
			),
			expectedCounter: 5,
			validator:       isNil,
		},
		{
			name: "FallbackToExecuted",
			step: OnError(
				IncrementAndFail(error1),
				FallbackTo(Increment(10)),
			),
			expectedCounter: 11, // 1 from fail + 10 from fallback
			validator:       isNil,
		},
		{
			name: "NilFallbackSwallowsError",
			step: OnError(
				IncrementAndFail(error1),
				func(_ context.Context, _ *CountingFlow, err error) (Step[*CountingFlow], error) {
					// `return nil, nil` is what we're testing!
					//nolint:nilnil
					return nil, nil // Swallow the error
				},
			),
			expectedCounter: 1,
			validator:       isNil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}
func TestWithPanicRecovery(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		step      Step[*CountingFlow]
		validator func(error) error
	}{
		{
			name:      "RecoversPanic",
			step:      RecoverPanics(PanicWith("oh no!")),
			validator: isRecoveredPanic,
		},
		{
			name:      "DoesNotAffectNormalOperation",
			step:      RecoverPanics(Increment(5)),
			validator: isNil,
		},
		{
			name:      "DoesNotAffectErrors",
			step:      RecoverPanics(IncrementAndFail(error1)),
			validator: matches(error1),
		},
		{
			name: "AllowsContinuationAfterPanic",
			step: Do(
				RecoverPanics(PanicWith("boom")),
				Increment(3),
			),
			validator: isRecoveredPanic,
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

	t.Run("ErrorMessage", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		err := RecoverPanics(PanicWith("test panic"))(t.Context(), &c)
		if err == nil {
			t.Fatal("expected error")
		}
		expectedMsg := "panic recovered: test panic"
		if err.Error() != expectedMsg {
			t.Errorf("expected error message %q, got %q",
				expectedMsg, err.Error())
		}
	})
}

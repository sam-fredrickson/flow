// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
)

// A Predicate is a failable boolean condition check.
//
// It returns true or false to indicate whether the condition is met,
// and may return an error if the condition check itself fails.
//
// T is some kind of state upon which the check operates.
//
// If using parallel steps, ensure that access to T is thread-safe.
type Predicate[T any] = func(context.Context, T) (bool, error)

// When runs the given step only if the predicate returns true.
//
// If the predicate returns false, the step is skipped and nil is returned.
// If the predicate returns an error, that error is propagated.
func When[T any](
	predicate Predicate[T],
	step Step[T],
) Step[T] {
	return func(ctx context.Context, t T) error {
		ok, err := predicate(ctx, t)
		if err != nil {
			return err
		}
		if ok {
			return step(ctx, t)
		}
		return nil
	}
}

// Unless runs the given step only if the predicate returns false.
//
// If the predicate returns true, the step is skipped and nil is returned.
// If the predicate returns an error, that error is propagated.
func Unless[T any](
	predicate Predicate[T],
	step Step[T],
) Step[T] {
	return func(ctx context.Context, t T) error {
		ok, err := predicate(ctx, t)
		if err != nil {
			return err
		}
		if !ok {
			return step(ctx, t)
		}
		return nil
	}
}

// While repeatedly executes the step as long as the predicate returns true.
//
// The predicate is evaluated before each iteration. If it returns false or
// an error, the loop terminates. If the step returns an error, that error
// is propagated immediately.
//
// Example:
//
//	While(
//	    ServiceIsNotReady(),
//	    CheckStatus(),
//	)
//
// To prevent infinite loops, combine with [WithTimeout] or use predicates that
// eventually become false.
func While[T any](
	predicate Predicate[T],
	step Step[T],
) Step[T] {
	return func(ctx context.Context, t T) error {
		for {
			ok, err := predicate(ctx, t)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			if err := step(ctx, t); err != nil {
				return err
			}
		}
	}
}

// Not negates a predicate, returning true when the predicate returns false
// and vice versa.
//
// Example:
//
//	While(Not(ServiceIsHealthy()), CheckAndWait())
func Not[T any](predicate Predicate[T]) Predicate[T] {
	return func(ctx context.Context, t T) (bool, error) {
		ok, err := predicate(ctx, t)
		return !ok, err
	}
}

// And combines multiple predicates with logical AND.
//
// All predicates must return true for the result to be true. Evaluation
// short-circuits on the first false or error.
//
// Example:
//
//	When(
//	    And(IsProduction(), IsHealthy()),
//	    DeployToProduction,
//	)
func And[T any](predicates ...Predicate[T]) Predicate[T] {
	return func(ctx context.Context, t T) (bool, error) {
		for _, p := range predicates {
			ok, err := p(ctx, t)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
}

// Or combines multiple predicates with logical OR.
//
// Returns true if any predicate returns true. Evaluation short-circuits
// on the first true or error.
//
// Example:
//
//	When(
//	    Or(IsDevEnvironment(), HasFeatureFlag()),
//	    EnableExperimentalFeature,
//	)
func Or[T any](predicates ...Predicate[T]) Predicate[T] {
	return func(ctx context.Context, t T) (bool, error) {
		for _, p := range predicates {
			ok, err := p(ctx, t)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
}

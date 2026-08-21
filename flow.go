// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
)

// A Step is a failable, cancelable procedure.
//
// T is some kind of state upon which the procedure operates.
//
// If using parallel steps, ensure that access to T is thread-safe.
type Step[T any] func(context.Context, T) error

// Extract is a failable, cancelable procedure that returns a value.
//
// T is some kind of state upon which the procedure operates.
// U is the returned value.
//
// If using parallel steps, ensure that access to T is thread-safe.
type Extract[T any, U any] func(context.Context, T) (U, error)

// A Transform is a failable, cancelable procedure that converts one input
// into an output.
//
// The error type is used to indicate that the transform failed.
//
// T is some kind of state upon which the procedure operates.
//
// If using parallel steps, ensure that access to T is thread-safe.
type Transform[T any, In any, Out any] func(context.Context, T, In) (Out, error)

// Consume is a failable, cancelable procedure that takes a parameter.
//
// T is some kind of state upon which the procedure operates.
// U is the input value to be consumed/processed.
//
// If using parallel steps, ensure that access to T is thread-safe.
type Consume[T any, U any] func(context.Context, T, U) error

// Pipeline composes an [Extract], [Transform], and [Consume] into a complete [Step].
//
// This creates a full data pipeline: extract a value from state, transform it,
// and consume the result. This is a common pattern for workflows that follow
// the read-process-write model.
//
// Pipeline is equivalent to:
//
//	With(From(extract, transform), consume)
//	With(extract, Feed(transform, consume))
//
// Example:
//
//	processConfig := Pipeline(
//	    GetRawConfig,      // Extract[*State, []byte]
//	    ParseAndValidate,  // Transform[*State, []byte, Config]
//	    SaveToDatabase,    // Consume[*State, Config]
//	)                      // Step[*State]
func Pipeline[T, A, B any](
	extract Extract[T, A],
	transform Transform[T, A, B],
	consume Consume[T, B],
) Step[T] {
	return func(ctx context.Context, t T) error {
		a, err := extract(ctx, t)
		if err != nil {
			return err
		}
		b, err := transform(ctx, t, a)
		if err != nil {
			return err
		}
		return consume(ctx, t, b)
	}
}

// With combines an [Extract] and a [Consume] into a Step.
//
// This creates a two-stage workflow: extract a value from state, then
// process/consume it. This is useful when you need to read some data and
// perform an action with it.
//
// Example:
//
//	processUsers := With(
//	    GetUsers,      // Extract[*State, []User]
//	    SendEmails,    // Consume[*State, []User]
//	)                  // Step[*State]
func With[T, U any](
	extract Extract[T, U],
	consume Consume[T, U],
) Step[T] {
	return func(ctx context.Context, t T) error {
		u, err := extract(ctx, t)
		if err != nil {
			return err
		}
		return consume(ctx, t, u)
	}
}

// Value lifts a constant value into an [Extract].
//
// This creates an Extract that ignores state and context, always returning
// the given value. It's the monadic return/pure operation for Extract,
// useful for closing over values in compositions.
//
// Example:
//
//	// Close over a slice in ForEach
//	flow.ForEach(
//	    flow.Value[*State](items),
//	    ProcessItem,
//	)
//
//	// Use in Pipeline
//	flow.Pipeline(
//	    flow.Value[*State](config),
//	    ValidateConfig,
//	    SaveConfig,
//	)
func Value[T, U any](u U) Extract[T, U] {
	return func(_ context.Context, _ T) (U, error) {
		return u, nil
	}
}

// From composes an [Extract] and a [Transform] to produce a new [Extract].
//
// This combines the "read side" of a data pipeline: extracting a value from
// state and transforming it into another value. The result is a new [Extract]
// that performs both operations.
//
// Example:
//
//	validatedConfig := From(
//	    GetRawConfig,      // Extract[*State, []byte]
//	    ParseAndValidate,  // Transform[*State, []byte, Config]
//	)                      // Extract[*State, Config]
func From[T, A, B any](
	extract Extract[T, A],
	transform Transform[T, A, B],
) Extract[T, B] {
	return func(ctx context.Context, t T) (B, error) {
		a, err := extract(ctx, t)
		if err != nil {
			var zero B
			return zero, err
		}
		return transform(ctx, t, a)
	}
}

// Chain composes two Transforms into a single [Transform].
//
// This creates a transformation pipeline where the output of the first
// transform becomes the input to the second. Both transforms have access
// to the same state T.
//
// For longer pipelines, use the [Transform.Then] method, which chains
// any number of transforms fluently:
//
//	Parse.Then(Validate).Then(Normalize)
//
// Example:
//
//	parseAndValidate := Chain(
//	    ParseJSON,    // Transform[*State, []byte, Config]
//	    ValidateConfig, // Transform[*State, Config, Config]
//	)                 // Transform[*State, []byte, Config]
func Chain[T, A, B, C any](
	first Transform[T, A, B],
	second Transform[T, B, C],
) Transform[T, A, C] {
	return func(ctx context.Context, t T, a A) (C, error) {
		b, err := first(ctx, t, a)
		if err != nil {
			var zero C
			return zero, err
		}
		return second(ctx, t, b)
	}
}

// Feed composes a [Transform] and a [Consume] to produce a new [Consume].
//
// This combines the "write side" of a data pipeline: transforming an input
// value and feeding it to a consumer. The result is a new [Consume] that accepts
// the original input type A.
//
// Example:
//
//	saveConfig := Feed(
//	    ParseConfig,  // Transform[*State, []byte, Config]
//	    SaveToDB,     // Consume[*State, Config]
//	)                 // Consume[*State, []byte]
func Feed[T, A, B any](
	transform Transform[T, A, B],
	consume Consume[T, B],
) Consume[T, A] {
	return func(ctx context.Context, t T, a A) error {
		b, err := transform(ctx, t, a)
		if err != nil {
			return err
		}
		return consume(ctx, t, b)
	}
}

// Spawn executes a step with a different state type derived from the parent.
//
// This is useful when you need to run a workflow in a narrower context. The
// derive function extracts or constructs child state from the parent, then
// the step runs with that child state.
//
// Example:
//
//	// Run database setup with DB-specific state
//	Spawn(
//	    PrepareDbConnection,    // Extract[*EnvSetup, *DbSetup]
//	    RunDbMigrations,        // Step[*DbSetup]
//	)                           // Step[*EnvSetup]
func Spawn[Parent, Child any](
	derive Extract[Parent, Child],
	step Step[Child],
) Step[Parent] {
	return func(ctx context.Context, parent Parent) error {
		child, err := derive(ctx, parent)
		if err != nil {
			return err
		}
		return step(ctx, child)
	}
}

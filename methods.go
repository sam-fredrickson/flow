// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"time"
)

// This file defines the fluent, method-based API for composing workflows.
//
// Every method here delegates to a package-level combinator; the two styles
// are interchangeable and can be mixed freely. The method form reads
// left-to-right in data-flow order:
//
//	GetRawConfig.Via(Parse).Via(Validate).To(Save)
//
// which is equivalent to:
//
//	Pipeline(GetRawConfig, Chain(Parse, Validate), Save)
//
// Methods that introduce a new type parameter (like [Extract.Via] and
// [Transform.Then]) require Go 1.27 generic methods.

// Then composes this transform with a subsequent one, producing a single
// [Transform] whose output type may differ from this one's.
//
// This is the method form of [Chain], and chains to any length:
//
//	Parse.Then(Validate).Then(Normalize).Then(Enrich)
func (f Transform[T, In, Out]) Then[Next any](next Transform[T, Out, Next]) Transform[T, In, Next] {
	return Chain(f, next)
}

// To feeds this transform's output into a consumer, producing a [Consume]
// that accepts this transform's input.
//
// This is the method form of [Feed]:
//
//	ParseConfig.To(SaveToDB) // Consume[*State, []byte]
func (f Transform[T, In, Out]) To(consume Consume[T, Out]) Consume[T, In] {
	return Feed(f, consume)
}

// Note: there are no Transform.Each / Consume.Each methods lifting a
// per-element function to its slice form ([Render], [Apply], and their
// parallel variants). A non-generic method is instantiated eagerly along
// with its receiver type, so a method signature mentioning
// Transform[T, []In, []Out] from within Transform[T, In, Out] triggers an
// infinite instantiation cycle ([]In → [][]In → ...), which the compiler
// rejects. Use the package-level functions instead:
//
//	GetUserIDs.Via(Render(LoadUser)).To(Apply(SaveUser))

// Via applies a transform to this extract's output, producing a new [Extract]
// whose value type may differ from this one's.
//
// This is the method form of [From], and chains to any length:
//
//	GetRawConfig.Via(Parse).Via(Validate) // Extract[*State, Config]
func (e Extract[T, U]) Via[Next any](transform Transform[T, U, Next]) Extract[T, Next] {
	return From(e, transform)
}

// To feeds this extract's output into a consumer, producing a complete [Step].
//
// This is the method form of [With]:
//
//	GetUsers.To(SendEmails) // Step[*State]
func (e Extract[T, U]) To(consume Consume[T, U]) Step[T] {
	return With(e, consume)
}

// Spawn executes a step with the child state this extract derives from the
// parent state.
//
// This is the method form of [Spawn]:
//
//	PrepareDbConnection.Spawn(RunDbMigrations) // Step[*EnvSetup]
func (e Extract[T, U]) Spawn(step Step[U]) Step[T] {
	return Spawn(e, step)
}

// Then runs this step followed by the next one, failing fast.
//
// This is the method form of [Do] for two steps. It reads best for pairing a
// step with one follow-up; for sequences of three or more steps, prefer the
// list form of [Do]:
//
//	CreateUser(name).Then(GrantAccess(name))
//
//	flow.Do(
//	    ValidateConfig(),
//	    StartDatabase(),
//	    StartServer(),
//	)
func (s Step[T]) Then(next Step[T]) Step[T] {
	return Do(s, next)
}

// Named wraps this step with a name; see [Named].
func (s Step[T]) Named(name string) Step[T] {
	return Named(name, s)
}

// Retry retries this step on failure; see [Retry].
func (s Step[T]) Retry(predicates ...RetryPredicate) Step[T] {
	return Retry(s, predicates...)
}

// When runs this step only if the predicate returns true; see [When].
//
//	Deploy.When(IsProduction)
func (s Step[T]) When(predicate Predicate[T]) Step[T] {
	return When(predicate, s)
}

// Unless runs this step only if the predicate returns false; see [Unless].
func (s Step[T]) Unless(predicate Predicate[T]) Step[T] {
	return Unless(predicate, s)
}

// While repeatedly executes this step as long as the predicate returns true;
// see [While].
func (s Step[T]) While(predicate Predicate[T]) Step[T] {
	return While(predicate, s)
}

// WithTimeout wraps this step with a timeout; see [WithTimeout].
func (s Step[T]) WithTimeout(timeout time.Duration) Step[T] {
	return WithTimeout(timeout, s)
}

// Scoped runs this step inside a new resource-cleanup scope; see [Scope].
//
//	flow.Do(
//	    flow.Manage(OpenConn, CloseConn),
//	    DoWork,
//	).Scoped()
func (s Step[T]) Scoped() Step[T] {
	return Scope(s)
}

// WithCleanupTimeout configures an independent timeout for cleanup within
// this step; see [WithCleanupTimeout].
func (s Step[T]) WithCleanupTimeout(timeout time.Duration) Step[T] {
	return WithCleanupTimeout(timeout, s)
}

// IgnoreError makes this step always return nil; see [IgnoreError].
func (s Step[T]) IgnoreError() Step[T] {
	return IgnoreError(s)
}

// OnError provides dynamic error handling with fallback steps; see [OnError].
func (s Step[T]) OnError(handler Transform[T, error, Step[T]]) Step[T] {
	return OnError(s, handler)
}

// Via applies a transform to each item pulled from this source, producing a
// new [Source] whose item type may differ from this one's.
//
// This is [From] specialized to the streaming protocol: each pull takes one
// item from the source and transforms it, and [ErrExhausted] passes through
// untouched. Unlike [Extract.Via] over slices, no collection is realized.
//
//	flow.Stream(FetchNextItem).Via(Validate).Via(Enrich)
func (s Source[T, U]) Via[Next any](transform Transform[T, U, Next]) Source[T, Next] {
	return Source[T, Next](From(Extract[T, U](s), transform))
}

// Drain consumes every item pulled from this source, serially; see [Drain].
//
//	flow.Stream(FetchNextItem).Drain(SaveItem) // Step[*State]
func (s Source[T, U]) Drain(consume Consume[T, U]) Step[T] {
	return Drain(s, consume)
}

// DrainParallel consumes items pulled from this source with a pool of
// concurrent workers; see [DrainParallel].
//
//	flow.Stream(FetchNextItem).DrainParallel(SaveItem, flow.ParallelOptions{Limit: 8})
func (s Source[T, U]) DrainParallel(consume Consume[T, U], opts ParallelOptions) Step[T] {
	return DrainParallel(s, consume, opts)
}

// Collect materializes this source into a slice-producing [Extract]; see
// [Collect]. Use it to exit the streaming world when a downstream stage
// genuinely needs the whole collection.
func (s Source[T, U]) Collect() Extract[T, []U] {
	return Collect(Extract[T, U](s))
}

// And combines this predicate with others using logical AND; see [And].
func (p Predicate[T]) And(others ...Predicate[T]) Predicate[T] {
	return And(append([]Predicate[T]{p}, others...)...)
}

// Or combines this predicate with others using logical OR; see [Or].
func (p Predicate[T]) Or(others ...Predicate[T]) Predicate[T] {
	return Or(append([]Predicate[T]{p}, others...)...)
}

// Not negates this predicate; see [Not].
func (p Predicate[T]) Not() Predicate[T] {
	return Not(p)
}

// Get retrieves this key's workflow-scoped value from the context.
//
// This is the method form of [Lookup].
func (k Key[V]) Get(ctx context.Context) (V, bool) {
	return Lookup(ctx, k)
}

// Set returns a [Step] that sets this key's workflow-scoped value before
// executing the given step.
//
// This is the method form of [WithValue]:
//
//	DryRun.Set(true, myWorkflow)
func (k Key[V]) Set[T any](val V, step Step[T]) Step[T] {
	return WithValue(k, val, step)
}

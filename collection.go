// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"fmt"
)

// IndexedError wraps an error with the index at which it occurred in a collection.
//
// This provides context when iterating over elements in functions like [Render],
// [Apply], or [Collect] fails, making it easier to debug which element caused
// the failure.
//
// Example:
//
//	err := flow.Render(ProcessItem)(ctx, state, items)
//	if err != nil {
//	    var ie *flow.IndexedError
//	    if errors.As(err, &ie) {
//	        fmt.Printf("Failed at element %d: %v\n", ie.Index, ie.Err)
//	    }
//	}
type IndexedError struct {
	Index int
	Err   error
}

// Error implements the error interface.
func (e *IndexedError) Error() string {
	return fmt.Sprintf("element %d: %v", e.Index, e.Err)
}

// Unwrap returns the underlying error for error inspection via errors.Is and errors.As.
func (e *IndexedError) Unwrap() error {
	return e.Err
}

// ErrExhausted is a sentinel error that signals extraction has been exhausted.
//
// This is used with [Collect] to indicate that an iterative extraction has
// reached its end and no more items are available.
//
// Example:
//
//	func NextItem(ctx context.Context, state *State) (Item, error) {
//	    if state.queue.IsEmpty() {
//	        return Item{}, flow.ErrExhausted
//	    }
//	    return state.queue.Pop(), nil
//	}
var ErrExhausted = errors.New("extraction exhausted")

// Map lifts a function from A to Step[T] into a [Transform] from []A to
// []Step[T].
//
// Map is designed to compose with [From] for dynamic workflows:
//
//	flow.InSerial(
//	    flow.From(
//	        GetServices,                 // Extract[*Config, []string]
//	        flow.Map(DeployService), // Transform[*Config, []string, []Step[*Config]]
//	    ),                               // → StepsProvider[*Config]
//	)
//
// The call to [From] results in a [StepsProvider], the same type used by [InSerial] and
// [InParallel]. This allows one to switch such a loop from serial to concurrent
// processing simply by changing the function that wraps [From].
//
// Map can also be used with [Chain] to build more complex transformations:
//
//	flow.From(
//	    GetRawData,                      // Extract[*State, Data]
//	    flow.Chain(
//	        ParseItems,                  // Transform[*State, Data, []Item]
//	        flow.Map(ProcessItem),   // Transform[*State, []Item, []Step[*State]]
//	    ),
//	)
func Map[T any, A any](
	f func(A) Step[T],
) Transform[T, []A, []Step[T]] {
	return func(ctx context.Context, t T, as []A) ([]Step[T], error) {
		steps := make([]Step[T], len(as))
		for i, a := range as {
			steps[i] = f(a)
		}
		return steps, nil
	}
}

// ForEach is a convenience helper that combines [Extract] and [Map].
//
// It extracts a slice and maps each element to a step, resulting in a [StepsProvider].
// This is useful when you want the flexibility to choose serial or parallel execution
// at the orchestration level.
//
// Example:
//
//	// Define once
//	processWorkflow := flow.ForEach(GetItems, ProcessItem)
//
//	// Use serially
//	flow.InSerial(processWorkflow)
//
//	// Use in parallel
//	flow.InParallel(processWorkflow)
func ForEach[T, U any](
	extract Extract[T, []U],
	f func(U) Step[T],
) StepsProvider[T] {
	return From(extract, Map(f))
}

// Collect repeatedly extracts until [ErrExhausted], collecting all results.
//
// This enables pull-based iteration patterns while staying within the Extract
// algebra. The extractor should return [ErrExhausted] when no more items are available.
//
// Example:
//
//	func NextQueueItem(ctx context.Context, state *State) (Item, error) {
//	    if state.queue.IsEmpty() {
//	        return Item{}, flow.ErrExhausted
//	    }
//	    return state.queue.Pop(), nil
//	}
//
//	// Collect all items from queue
//	allItems := Collect(NextQueueItem)  // Extract[*State, []Item]
//
//	// Use in pipeline
//	flow.Pipeline(
//	    Collect(NextQueueItem),
//	    Render(TransformItem),
//	    Apply(SaveItem),
//	)
func Collect[T, U any](f Extract[T, U]) Extract[T, []U] {
	return func(ctx context.Context, t T) ([]U, error) {
		var collected []U

		for i := 0; ; i++ {
			// Check for cancellation
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			item, err := f(ctx, t)
			if err != nil {
				if errors.Is(err, ErrExhausted) {
					return collected, nil
				}
				return nil, &IndexedError{Index: i, Err: err}
			}

			collected = append(collected, item)
		}
	}
}

// Render transforms each element in a slice (serial, fail-fast).
//
// This completes the Transform algebra with collection support. It enables
// natural composition in [From] and [Chain] for processing slices.
//
// Example:
//
//	// Multi-stage rendering pipeline
//	flow.Pipeline(
//	    GetRawData,                           // Extract[T, [][]byte]
//	    flow.Chain(
//	        Render(Parse),                    // []byte → []Record
//	        Render(Validate),                 // []Record → []Record
//	        Render(Enrich),                   // []Record → []EnrichedRecord
//	    ),
//	    Apply(Save),
//	)
//
//	// Simple transformation
//	flow.From(
//	    GetUserIDs,                           // Extract[T, []int]
//	    Render(LoadUser),                     // Transform[T, int, User]
//	)                                         // Extract[T, []User]
func Render[T, In, Out any](f Transform[T, In, Out]) Transform[T, []In, []Out] {
	return func(ctx context.Context, t T, items []In) ([]Out, error) {
		results := make([]Out, len(items))

		for i, item := range items {
			// Check for cancellation
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			out, err := f(ctx, t, item)
			if err != nil {
				return nil, &IndexedError{Index: i, Err: err}
			}
			results[i] = out
		}

		return results, nil
	}
}

// Apply consumes each element in a slice (serial, fail-fast).
//
// This enables clean serial iteration without orchestrator wrappers. It's
// fully compositional with existing [Feed] and [Pipeline].
//
// Example:
//
//	// Simple: extract and apply
//	flow.With(
//	    GetUsers,                             // Extract[T, []User]
//	    Apply(SaveUser),                      // Consume[T, User]
//	)                                         // Step[T]
//
//	// Pipeline: extract, transform, apply
//	flow.Pipeline(
//	    GetIDs,                               // Extract[T, []int]
//	    Render(LoadEntity),                   // Transform to []Entity
//	    Apply(ProcessEntity),                 // Consume each
//	)
//
//	// Composed consumption
//	flow.With(
//	    GetRecords,
//	    Apply(flow.Feed(
//	        ValidateRecord,
//	        SaveRecord,
//	    )),
//	)
func Apply[T, U any](f Consume[T, U]) Consume[T, []U] {
	return func(ctx context.Context, t T, items []U) error {
		for i, item := range items {
			// Check for cancellation
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := f(ctx, t, item); err != nil {
				return &IndexedError{Index: i, Err: err}
			}
		}
		return nil
	}
}

// Flatten flattens a nested slice structure.
//
// This is commonly needed when using [Render] on a function that returns slices,
// which produces nested slices ([][]Out). Flatten collapses these into a single
// slice, making it easy to compose in transformation pipelines.
//
// Example:
//
//	// Collect pages, extract records from each page (gives [][]Record), flatten
//	flow.From(
//	    flow.Collect(FetchPages),        // Extract[*State, []Page]
//	    flow.Chain(
//	        flow.Render(ExtractRecords), // []Page → [][]Record
//	        flow.Flatten,                // [][]Record → []Record
//	        flow.Render(ValidateRecord), // []Record → []ValidatedRecord
//	    ),
//	)
//
// Common use case: multi-stage ETL pipelines where one transformation produces
// multiple items per input:
//
//	flow.Pipeline(
//	    GetBatches,                           // Extract[*State, []Batch]
//	    flow.Chain(
//	        flow.Render(SplitBatchIntoItems), // Batch → []Item, gives [][]Item
//	        flow.Flatten,                     // [][]Item → []Item
//	        flow.Render(ProcessItem),         // []Item → []ProcessedItem
//	    ),
//	    flow.Apply(SaveItem),
//	)
func Flatten[T, U any](_ context.Context, _ T, nested [][]U) ([]U, error) {
	var flattened []U
	for _, slice := range nested {
		flattened = append(flattened, slice...)
	}
	return flattened, nil
}

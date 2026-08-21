// SPDX-License-Identifier: Apache-2.0

// Package main demonstrates using workflow-scoped key-value storage to
// implement tag-based selective execution.
//
// Steps are tagged with categories (e.g., "db", "cache"), and the workflow
// is configured to only run steps matching certain tags.
package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/sam-fredrickson/flow"
)

// ActiveTags is the workflow-scoped key for the set of active tags.
var ActiveTags = flow.NewKey[[]string]("active-tags")

// WithTags sets the active tags for a workflow.
func WithTags[T any](tags []string, step flow.Step[T]) flow.Step[T] {
	return ActiveTags.Set(tags, step)
}

// HasTag checks if a specific tag is in the active tag set.
func HasTag(ctx context.Context, tag string) bool {
	tags, ok := ActiveTags.Get(ctx)
	if !ok {
		return false
	}
	return slices.Contains(tags, tag)
}

// Tagged wraps a step so it only executes when the given tag is active.
// If no tags are set, the step always runs (opt-in filtering).
func Tagged[T any](tag string, step flow.Step[T]) flow.Step[T] {
	return func(ctx context.Context, t T) error {
		_, hasTags := ActiveTags.Get(ctx)
		if hasTags && !HasTag(ctx, tag) {
			fmt.Printf("  [skip] %s (tag %q not active)\n", tag, tag)
			return nil
		}
		return step(ctx, t)
	}
}

// State is a simple workflow state.
type State struct{}

func main() {
	setupDB := Tagged("db",
		func(ctx context.Context, s *State) error {
			fmt.Println("  [run]  Setting up database connection")
			return nil
		},
	).Named("setup-db")

	migrateDB := Tagged("db",
		func(ctx context.Context, s *State) error {
			fmt.Println("  [run]  Running database migrations")
			return nil
		},
	).Named("migrate-db")

	warmCache := Tagged("cache",
		func(ctx context.Context, s *State) error {
			fmt.Println("  [run]  Warming cache")
			return nil
		},
	).Named("warm-cache")

	flushCache := Tagged("cache",
		func(ctx context.Context, s *State) error {
			fmt.Println("  [run]  Flushing cache")
			return nil
		},
	).Named("flush-cache")

	workflow := flow.Do(setupDB, migrateDB, warmCache, flushCache)

	// Run with only "db" tags active — cache steps are skipped.
	fmt.Println("=== Running with tags: [db] ===")
	err := WithTags([]string{"db"}, workflow)(context.Background(), &State{})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Run with all tags — everything runs.
	fmt.Println("\n=== Running with tags: [db, cache] ===")
	err = WithTags([]string{"db", "cache"}, workflow)(context.Background(), &State{})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Run without tags — everything runs (opt-in filtering).
	fmt.Println("\n=== Running without tags (no filtering) ===")
	err = workflow(context.Background(), &State{})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
}

// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTraceFilters(t *testing.T) {
	t.Parallel()

	// Create a workflow with steps of varying durations
	createWorkflow := func() Step[*CountingFlow] {
		return Do(
			Named("fast", Sleep[*CountingFlow](1*time.Millisecond)),
			Named("slow", Sleep[*CountingFlow](50*time.Millisecond)),
			IgnoreError(Named("error", func(ctx context.Context, t *CountingFlow) error {
				return errors.New("test error")
			})),
			Named("fast2", Sleep[*CountingFlow](1*time.Millisecond)),
		)
	}

	testCases := []struct {
		name          string
		filter        TraceFilter
		expectedCount int
		checkFunc     func(*testing.T, []TraceEvent)
	}{
		{
			name:          "MinDuration",
			filter:        MinDuration(20 * time.Millisecond),
			expectedCount: 1,
			checkFunc: func(t *testing.T, events []TraceEvent) {
				if events[0].Names[0] != "slow" {
					t.Errorf("expected slow step, got %v", events[0].Names)
				}
			},
		},
		{
			name:          "MaxDuration",
			filter:        MaxDuration(10 * time.Millisecond),
			expectedCount: 3, // fast, error (instant), fast2
			checkFunc: func(t *testing.T, events []TraceEvent) {
				for _, event := range events {
					if event.Names[0] == "slow" {
						t.Error("slow step should be filtered out")
					}
				}
			},
		},
		{
			name:          "HasError",
			filter:        HasError(),
			expectedCount: 1,
			checkFunc: func(t *testing.T, events []TraceEvent) {
				if events[0].Names[0] != "error" {
					t.Errorf("expected error step, got %v", events[0].Names)
				}
			},
		},
		{
			name:          "NoError",
			filter:        NoError(),
			expectedCount: 3,
			checkFunc: func(t *testing.T, events []TraceEvent) {
				for _, event := range events {
					if event.Error != "" {
						t.Errorf("expected no error, got %s", event.Error)
					}
				}
			},
		},
		{
			name:          "NameMatches wildcard",
			filter:        NameMatches("fast*"),
			expectedCount: 2,
			checkFunc: func(t *testing.T, events []TraceEvent) {
				for _, event := range events {
					if !strings.HasPrefix(event.Names[0], "fast") {
						t.Errorf("expected name to start with 'fast', got %s", event.Names[0])
					}
				}
			},
		},
		{
			name:          "NamePrefix",
			filter:        NamePrefix("fast"),
			expectedCount: 2,
			checkFunc:     nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			state := &CountingFlow{}
			trace, _ := Traced(createWorkflow())(ctx, state)

			filtered := trace.Filter(tc.filter)
			events := filtered.Events

			if len(events) != tc.expectedCount {
				t.Fatalf("expected %d events, got %d", tc.expectedCount, len(events))
			}

			if tc.checkFunc != nil {
				tc.checkFunc(t, events)
			}
		})
	}

	t.Run("multiple filters", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		state := &CountingFlow{}
		trace, _ := Traced(createWorkflow())(ctx, state)

		// Find fast steps without errors
		filtered := trace.Filter(
			NamePrefix("fast"),
			NoError(),
			MaxDuration(10*time.Millisecond),
		)
		events := filtered.Events

		if len(events) != 2 {
			t.Fatalf("expected 2 events matching all filters, got %d", len(events))
		}
	})
}

func TestTracePathFilters(t *testing.T) {
	t.Parallel()

	// Create a nested workflow
	workflow := Named("parent", Do(
		Named("child1", func(ctx context.Context, t *CountingFlow) error {
			return nil
		}),
		Named("child2", Do(
			Named("grandchild", func(ctx context.Context, t *CountingFlow) error {
				return nil
			}),
		)),
	))

	testCases := []struct {
		name          string
		filter        TraceFilter
		expectedCount int
	}{
		{
			name:          "PathMatches all children",
			filter:        PathMatches("parent.*"),
			expectedCount: 3, // parent.child1, parent.child2, parent.child2.grandchild
		},
		{
			name:          "HasPathPrefix",
			filter:        HasPathPrefix([]string{"parent", "child2"}),
			expectedCount: 2, // parent.child2 and parent.child2.grandchild
		},
		{
			name:          "DepthEquals 2",
			filter:        DepthEquals(2),
			expectedCount: 2, // parent.child1, parent.child2
		},
		{
			name:          "DepthAtMost 2",
			filter:        DepthAtMost(2),
			expectedCount: 3, // parent (1), child1 (2), child2 (2)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			state := &CountingFlow{}
			trace, _ := Traced(workflow)(ctx, state)

			filtered := trace.Filter(tc.filter)
			events := filtered.Events

			if len(events) != tc.expectedCount {
				t.Fatalf("expected %d events, got %d", tc.expectedCount, len(events))
			}
		})
	}
}

func TestTimeRangeFilter(t *testing.T) {
	t.Parallel()

	workflow := Do(
		Named("step1", Sleep[*CountingFlow](10*time.Millisecond)),
		Named("step2", Sleep[*CountingFlow](10*time.Millisecond)),
		Named("step3", Sleep[*CountingFlow](10*time.Millisecond)),
	)

	ctx := t.Context()
	state := &CountingFlow{}
	trace, _ := Traced(workflow)(ctx, state)

	events := trace.Events
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	testCases := []struct {
		name          string
		start         time.Time
		end           time.Time
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "filter events in time range",
			start:         events[0].Start,
			end:           events[1].Start.Add(1 * time.Millisecond),
			expectedCount: 2,
			expectedNames: []string{"step1", "step2"},
		},
		{
			name:          "filter with narrow time range",
			start:         events[0].Start,
			end:           events[0].Start.Add(1 * time.Nanosecond),
			expectedCount: 1,
			expectedNames: nil,
		},
		{
			name:          "filter with time range before all events",
			start:         events[0].Start.Add(-1 * time.Hour),
			end:           events[0].Start.Add(-30 * time.Minute),
			expectedCount: 0,
			expectedNames: nil,
		},
		{
			name:          "filter with time range after all events",
			start:         events[2].Start.Add(1 * time.Hour),
			end:           events[2].Start.Add(2 * time.Hour),
			expectedCount: 0,
			expectedNames: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filtered := trace.Filter(TimeRange(tc.start, tc.end))
			filteredEvents := filtered.Events

			if len(filteredEvents) != tc.expectedCount {
				t.Errorf("expected %d events in range, got %d", tc.expectedCount, len(filteredEvents))
			}

			if tc.expectedNames != nil {
				for i, name := range tc.expectedNames {
					if filteredEvents[i].Names[0] != name {
						t.Errorf("expected %s, got %v", name, filteredEvents[i].Names)
					}
				}
			}
		})
	}
}

func TestErrorMatchesFilter(t *testing.T) {
	t.Parallel()

	workflow := Do(
		IgnoreError(Named("timeout-error", func(ctx context.Context, t *CountingFlow) error {
			return errors.New("connection timeout")
		})),
		IgnoreError(Named("db-error", func(ctx context.Context, t *CountingFlow) error {
			return errors.New("database connection failed")
		})),
		IgnoreError(Named("validation-error", func(ctx context.Context, t *CountingFlow) error {
			return errors.New("validation failed: invalid input")
		})),
		Named("success", Increment(1)),
	)

	ctx := t.Context()
	state := &CountingFlow{}
	trace, _ := Traced(workflow)(ctx, state)

	testCases := []struct {
		name          string
		pattern       string
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "match errors with wildcard pattern",
			pattern:       "*timeout*",
			expectedCount: 1,
			expectedNames: []string{"timeout-error"},
		},
		{
			name:          "match errors with prefix pattern",
			pattern:       "database*",
			expectedCount: 1,
			expectedNames: []string{"db-error"},
		},
		{
			name:          "match errors with complex pattern",
			pattern:       "*connection*",
			expectedCount: 2,
			expectedNames: nil, // Don't check names, just count
		},
		{
			name:          "no match for non-error events",
			pattern:       "*",
			expectedCount: 3, // Should match 3 error events, not the success event
			expectedNames: nil,
		},
		{
			name:          "invalid pattern matches nothing",
			pattern:       "[invalid",
			expectedCount: 0,
			expectedNames: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filtered := trace.Filter(ErrorMatches(tc.pattern))
			events := filtered.Events

			if len(events) != tc.expectedCount {
				t.Fatalf("expected %d events matching %s, got %d", tc.expectedCount, tc.pattern, len(events))
			}

			if tc.expectedNames != nil {
				for i, name := range tc.expectedNames {
					if events[i].Names[0] != name {
						t.Errorf("expected %s, got %v", name, events[i].Names)
					}
				}
			}

			// For the wildcard pattern, verify all matched events have errors
			if tc.pattern == "*" {
				for _, event := range events {
					if event.Error == "" {
						t.Error("expected all events to have errors")
					}
				}
			}
		})
	}

	t.Run("combine with other filters", func(t *testing.T) {
		t.Parallel()

		filtered := trace.Filter(
			ErrorMatches("*connection*"),
			NamePrefix("db"),
		)
		events := filtered.Events

		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		if events[0].Names[0] != "db-error" {
			t.Errorf("expected db-error, got %v", events[0].Names)
		}
	})
}

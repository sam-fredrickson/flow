// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTraced(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		step       Step[*CountingFlow]
		validators []traceValidator
	}{
		{
			name: "single named step",
			step: Named("test", Increment(1)),
			validators: []traceValidator{
				expectEvents(1),
				expectEventNames("test"),
			},
		},
		{
			name: "multiple named steps",
			step: Do(
				Named("step1", Increment(1)),
				Named("step2", Increment(1)),
				Named("step3", Increment(1)),
			),
			validators: []traceValidator{
				expectEvents(3),
				expectEventNames("step1", "step2", "step3"),
			},
		},
		{
			name: "nested named steps",
			step: Named("parent", Do(
				Named("child1", Increment(1)),
				Named("child2", Increment(1)),
			)),
			validators: []traceValidator{
				expectEvents(3),
				expectEventPath(0, []string{"parent"}),
				expectEventPath(1, []string{"parent", "child1"}),
				expectEventPath(2, []string{"parent", "child2"}),
			},
		},
		{
			name: "step with error",
			step: Named("failing", func(ctx context.Context, t *CountingFlow) error {
				return errors.New("test error")
			}),
			validators: []traceValidator{
				expectEvents(1),
				expectErrorCount(1),
			},
		},
		{
			name: "unnamed steps are not traced",
			step: Do(
				Increment(1),
				Named("named", Increment(1)),
				Increment(1),
			),
			validators: []traceValidator{
				expectEvents(1),
				expectEventNames("named"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runTraceTest(t, tc.step, tc.validators...)
		})
	}
}

func TestNamedVariantsTracing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		step       Step[*CountingFlow]
		validators []traceValidator
	}{
		{
			name: "NamedExtract",
			step: With(
				NamedExtract("extract", GetCount),
				SendCount,
			),
			validators: []traceValidator{
				expectEvents(1),
				expectEventNames("extract"),
			},
		},
		{
			name: "NamedTransform",
			step: Pipeline(
				func(ctx context.Context, t *CountingFlow) (int, error) {
					return 42, nil
				},
				NamedTransform("transform", func(ctx context.Context, t *CountingFlow, in int) (string, error) {
					return "result", nil
				}),
				func(ctx context.Context, t *CountingFlow, s string) error {
					return nil
				},
			),
			validators: []traceValidator{
				expectEvents(1),
				expectEventNames("transform"),
			},
		},
		{
			name: "NamedConsume",
			step: Pipeline(
				func(ctx context.Context, t *CountingFlow) (int, error) {
					return 42, nil
				},
				func(ctx context.Context, t *CountingFlow, in int) (int, error) {
					return in, nil
				},
				NamedConsume("consume", func(ctx context.Context, t *CountingFlow, val int) error {
					return nil
				}),
			),
			validators: []traceValidator{
				expectEvents(1),
				expectEventNames("consume"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runTraceTest(t, tc.step, tc.validators...)
		})
	}
}

func TestTraceQueryMethods(t *testing.T) {
	t.Parallel()

	workflow := Do(
		Named("step1", Sleep[*CountingFlow](10*time.Millisecond)),
		IgnoreError(Named("step2", func(ctx context.Context, t *CountingFlow) error {
			return errors.New("error")
		})),
		Named("step3", Sleep[*CountingFlow](10*time.Millisecond)),
	)

	ctx := t.Context()
	state := &CountingFlow{}
	trace, _ := Traced(workflow)(ctx, state)

	testCases := []struct {
		name      string
		checkFunc func(*testing.T, *Trace)
	}{
		{
			name: "TotalSteps",
			checkFunc: func(t *testing.T, trace *Trace) {
				if trace.TotalSteps != 3 {
					t.Errorf("expected 3 steps, got %d", trace.TotalSteps)
				}
			},
		},
		{
			name: "TotalErrors",
			checkFunc: func(t *testing.T, trace *Trace) {
				if trace.TotalErrors != 1 {
					t.Errorf("expected 1 error, got %d", trace.TotalErrors)
				}
			},
		},
		{
			name: "Duration",
			checkFunc: func(t *testing.T, trace *Trace) {
				duration := trace.Duration
				if duration < 10*time.Millisecond {
					t.Errorf("expected duration >= 10ms, got %v", duration)
				}
			},
		},
		{
			name: "Events field is directly accessible",
			checkFunc: func(t *testing.T, trace *Trace) {
				// Events is now a public field, not a method
				events := trace.Events
				if len(events) != 3 {
					t.Errorf("expected 3 events, got %d", len(events))
				}
				// Verify we can access fields directly
				if events[0].Names == nil {
					t.Error("expected event to have Names")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.checkFunc(t, trace)
		})
	}
}

func TestTraceThreadSafety(t *testing.T) {
	t.Parallel()

	// Create a workflow with parallel execution
	workflow := InParallel(Steps(
		Named("step1", Do(Sleep[*CountingFlow](10*time.Millisecond), Increment(1))),
		Named("step2", Do(Sleep[*CountingFlow](10*time.Millisecond), Increment(1))),
		Named("step3", Do(Sleep[*CountingFlow](10*time.Millisecond), Increment(1))),
		Named("step4", Do(Sleep[*CountingFlow](10*time.Millisecond), Increment(1))),
	))

	ctx := t.Context()
	state := &CountingFlow{}
	trace, err := Traced(workflow)(ctx, state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify all events were recorded
	events := trace.Events
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	// Concurrent access to trace fields
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = trace.Events
			_ = trace.TotalSteps
			_ = trace.TotalErrors
			_ = trace.Duration
			_ = trace.Filter(NoError())
		}()
	}
	wg.Wait()
}

func TestSpawnWithTraced(t *testing.T) {
	t.Parallel()

	workflow := Named("parent", Do(
		Named("child", func(ctx context.Context, t *CountingFlow) error {
			t.Counter++
			return nil
		}),
	))

	var buf bytes.Buffer
	combined := Spawn(
		Traced(workflow),
		WriteTextTo(&buf),
	)

	ctx := t.Context()
	state := &CountingFlow{}
	err := combined(ctx, state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify trace was written
	if buf.Len() == 0 {
		t.Error("expected trace output")
	}

	output := buf.String()
	if !strings.Contains(output, "parent") || !strings.Contains(output, "child") {
		t.Errorf("expected output to contain parent and child, got: %s", output)
	}
}

func TestTraceEdgeCases(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		step      Step[*CountingFlow]
		checkFunc func(*testing.T, *Trace, error)
	}{
		{
			name: "empty trace - no named steps",
			step: Do(
				Increment(1),
				Increment(1),
				Increment(1),
			),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				events := trace.Events
				if len(events) != 0 {
					t.Errorf("expected 0 events for empty trace, got %d", len(events))
				}
				if trace.TotalSteps != 0 {
					t.Errorf("expected TotalSteps == 0, got %d", trace.TotalSteps)
				}
				if trace.TotalErrors != 0 {
					t.Errorf("expected TotalErrors == 0, got %d", trace.TotalErrors)
				}

				// Ensure output methods don't panic on empty trace
				var buf bytes.Buffer
				n, err := trace.WriteTo(&buf)
				if err != nil {
					t.Errorf("WriteTo failed on empty trace: %v", err)
				}
				if n == 0 {
					t.Error("expected non-zero bytes for empty JSON array")
				}

				buf.Reset()
				_, err = trace.WriteText(&buf)
				if err != nil {
					t.Errorf("WriteText failed on empty trace: %v", err)
				}
				if buf.Len() != 0 {
					t.Errorf("expected no output for empty trace, got %d bytes", buf.Len())
				}
			},
		},
		{
			name: "filter that matches nothing",
			step: Named("test", Increment(1)),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				filtered := trace.Filter(NameMatches("nonexistent"))
				events := filtered.Events
				if len(events) != 0 {
					t.Errorf("expected 0 events from non-matching filter, got %d", len(events))
				}
				if filtered.Duration != 0 {
					t.Errorf("expected Duration == 0 on filtered trace, got %v", filtered.Duration)
				}
			},
		},
		{
			name: "invalid glob patterns return no matches",
			step: Do(
				Named("test-step", Increment(1)),
				Named("another-step", Increment(1)),
			),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				// Invalid glob pattern (malformed brackets)
				filtered := trace.Filter(NameMatches("[invalid"))
				events := filtered.Events
				if len(events) != 0 {
					t.Errorf("expected 0 events from invalid pattern, got %d", len(events))
				}

				// Same for PathMatches
				filtered = trace.Filter(PathMatches("[invalid"))
				events = filtered.Events
				if len(events) != 0 {
					t.Errorf("expected 0 events from invalid path pattern, got %d", len(events))
				}
			},
		},
		{
			name: "unicode and special characters in step names",
			step: Do(
				Named("hello-世界", Increment(1)),
				Named("test/with/slashes", Increment(1)),
				Named("dots.in.name", Increment(1)),
				Named("émojis-😀", Increment(1)),
			),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				if err != nil {
					t.Fatalf("expected no error with unicode names, got %v", err)
				}

				events := trace.Events
				if len(events) != 4 {
					t.Fatalf("expected 4 events, got %d", len(events))
				}

				expectedNames := []string{"hello-世界", "test/with/slashes", "dots.in.name", "émojis-😀"}
				for i, event := range events {
					if event.Names[0] != expectedNames[i] {
						t.Errorf("event %d: expected name %q, got %q", i, expectedNames[i], event.Names[0])
					}
				}

				// Verify JSON output handles unicode correctly
				var buf bytes.Buffer
				_, err = trace.WriteTo(&buf)
				if err != nil {
					t.Fatalf("WriteTo failed with unicode: %v", err)
				}

				// Parse back to verify round-trip
				var parsedEvents []TraceEvent
				if err := json.Unmarshal(buf.Bytes(), &parsedEvents); err != nil {
					t.Fatalf("failed to parse JSON with unicode: %v", err)
				}

				for i, event := range parsedEvents {
					if event.Names[0] != expectedNames[i] {
						t.Errorf("parsed event %d: expected name %q, got %q", i, expectedNames[i], event.Names[0])
					}
				}

				// Verify text output handles unicode
				buf.Reset()
				_, err = trace.WriteText(&buf)
				if err != nil {
					t.Fatalf("WriteText failed with unicode: %v", err)
				}

				output := buf.String()
				for _, name := range expectedNames {
					if !strings.Contains(output, name) {
						t.Errorf("text output missing name: %s", name)
					}
				}
			},
		},
		{
			name: "large trace with many events",
			step: func() Step[*CountingFlow] {
				const eventCount = 1000
				steps := make([]Step[*CountingFlow], eventCount)
				for i := 0; i < eventCount; i++ {
					name := strings.Repeat("x", i%100+1)
					steps[i] = Named(name, Increment(1))
				}
				return Do(steps...)
			}(),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}

				const eventCount = 1000
				events := trace.Events
				if len(events) != eventCount {
					t.Errorf("expected %d events, got %d", eventCount, len(events))
				}

				// Test filtering on large trace
				filtered := trace.Filter(DepthEquals(1))
				if len(filtered.Events) != eventCount {
					t.Errorf("expected %d filtered events, got %d", eventCount, len(filtered.Events))
				}

				// Test output methods don't panic or fail on large traces
				var buf bytes.Buffer
				_, err = trace.WriteTo(&buf)
				if err != nil {
					t.Errorf("WriteTo failed on large trace: %v", err)
				}

				buf.Reset()
				_, err = trace.WriteText(&buf)
				if err != nil {
					t.Errorf("WriteText failed on large trace: %v", err)
				}
			},
		},
		{
			name: "errors with special characters and newlines",
			step: Named("test", func(ctx context.Context, t *CountingFlow) error {
				return errors.New("error with\nnewlines\tand\ttabs and \"quotes\"")
			}),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				if err == nil {
					t.Fatal("expected error")
				}

				events := trace.Events
				if len(events) != 1 {
					t.Fatalf("expected 1 event, got %d", len(events))
				}

				// Verify error message is preserved
				if !strings.Contains(events[0].Error, "newlines") {
					t.Errorf("error message not preserved: %s", events[0].Error)
				}

				// Verify JSON output handles special characters
				var buf bytes.Buffer
				_, err = trace.WriteTo(&buf)
				if err != nil {
					t.Fatalf("WriteTo failed: %v", err)
				}

				// Parse back to verify round-trip
				var parsedEvents []TraceEvent
				if err := json.Unmarshal(buf.Bytes(), &parsedEvents); err != nil {
					t.Fatalf("failed to parse JSON with special chars: %v", err)
				}

				if !strings.Contains(parsedEvents[0].Error, "newlines") {
					t.Errorf("error message lost in JSON round-trip: %s", parsedEvents[0].Error)
				}
			},
		},
		{
			name: "empty path prefix matches all",
			step: Do(
				Named("step1", Increment(1)),
				Named("step2", Increment(1)),
			),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				filtered := trace.Filter(HasPathPrefix([]string{}))
				events := filtered.Events
				if len(events) != 2 {
					t.Errorf("expected empty prefix to match all 2 events, got %d", len(events))
				}
			},
		},
		{
			name: "negative and zero depth filters",
			step: Named("test", Increment(1)),
			checkFunc: func(t *testing.T, trace *Trace, err error) {
				filters := []struct {
					name   string
					filter TraceFilter
				}{
					{"DepthEquals(0)", DepthEquals(0)},
					{"DepthEquals(-1)", DepthEquals(-1)},
					{"DepthAtMost(0)", DepthAtMost(0)},
					{"DepthAtMost(-1)", DepthAtMost(-1)},
				}

				for _, f := range filters {
					filtered := trace.Filter(f.filter)
					if len(filtered.Events) != 0 {
						t.Errorf("%s: expected 0 events, got %d", f.name, len(filtered.Events))
					}
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			state := &CountingFlow{}
			trace, err := Traced(tc.step)(ctx, state)
			tc.checkFunc(t, trace, err)
		})
	}
}

func TestTracedStreaming(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		workflow  Step[*CountingFlow]
		checkFunc func(*testing.T, *Trace, *bytes.Buffer)
	}{
		{
			name: "stream events to writer",
			workflow: Do(
				Named("step1", Increment(1)),
				Named("step2", Increment(1)),
				Named("step3", Increment(1)),
			),
			checkFunc: func(t *testing.T, trace *Trace, buf *bytes.Buffer) {
				output := buf.String()
				if output == "" {
					t.Fatal("expected streamed output, got empty buffer")
				}

				// Parse streamed JSON Lines
				lines := strings.Split(strings.TrimSpace(output), "\n")
				if len(lines) != 3 {
					t.Fatalf("expected 3 JSON lines, got %d", len(lines))
				}

				// Verify each line is valid JSON
				for i, line := range lines {
					var event TraceEvent
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Errorf("line %d: failed to parse JSON: %v", i, err)
					}
				}

				// Verify trace still has events in memory
				events := trace.Events
				if len(events) != 3 {
					t.Errorf("expected 3 events in memory, got %d", len(events))
				}
			},
		},
		{
			name: "stream events with errors",
			workflow: Do(
				Named("step1", Increment(1)),
				IgnoreError(Named("step2", func(ctx context.Context, t *CountingFlow) error {
					return errors.New("test error")
				})),
				Named("step3", Increment(1)),
			),
			checkFunc: func(t *testing.T, trace *Trace, buf *bytes.Buffer) {
				lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
				if len(lines) != 3 {
					t.Fatalf("expected 3 JSON lines, got %d", len(lines))
				}

				// Check that step2 has error in streamed output
				var step2Event TraceEvent
				if err := json.Unmarshal([]byte(lines[1]), &step2Event); err != nil {
					t.Fatalf("failed to parse step2 JSON: %v", err)
				}

				if step2Event.Error == "" {
					t.Error("expected error in streamed step2 event")
				}

				// Verify error count
				if trace.TotalErrors != 1 {
					t.Errorf("expected 1 error, got %d", trace.TotalErrors)
				}
			},
		},
		{
			name: "streaming with nested steps",
			workflow: Named("parent", Do(
				Named("child1", Increment(1)),
				Named("child2", Increment(1)),
			)),
			checkFunc: func(t *testing.T, trace *Trace, buf *bytes.Buffer) {
				lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
				if len(lines) != 3 {
					t.Fatalf("expected 3 JSON lines, got %d", len(lines))
				}

				// Verify nested structure is preserved in streamed output
				var events []TraceEvent
				for _, line := range lines {
					var event TraceEvent
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatalf("failed to parse JSON: %v", err)
					}
					events = append(events, event)
				}

				// Events are streamed in finish order
				if len(events[0].Names) != 2 || events[0].Names[0] != "parent" || events[0].Names[1] != "child1" {
					t.Errorf("event 0: expected [parent child1], got %v", events[0].Names)
				}
				if len(events[1].Names) != 2 || events[1].Names[0] != "parent" || events[1].Names[1] != "child2" {
					t.Errorf("event 1: expected [parent child2], got %v", events[1].Names)
				}
				if len(events[2].Names) != 1 || events[2].Names[0] != "parent" {
					t.Errorf("event 2: expected [parent], got %v", events[2].Names)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			state := &CountingFlow{}

			var buf bytes.Buffer
			trace, err := Traced(tc.workflow, WithStreamTo(&buf))(ctx, state)

			if err != nil && !strings.Contains(tc.name, "error") {
				t.Fatalf("expected no error, got %v", err)
			}

			tc.checkFunc(t, trace, &buf)
		})
	}
}

func TestTracedEmptyWorkflow(t *testing.T) {
	t.Parallel()

	// Workflow with no Named steps should produce empty trace
	workflow := Do(
		Increment(1),
		Increment(1),
	)

	ctx := t.Context()
	state := &CountingFlow{}

	trace, err := Traced(workflow)(ctx, state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(trace.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(trace.Events))
	}

	if trace.TotalSteps != 0 {
		t.Errorf("expected TotalSteps=0, got %d", trace.TotalSteps)
	}

	if state.Counter != 2 {
		t.Errorf("expected counter=2, got %d", state.Counter)
	}
}

func TestTracedContextCancellation(t *testing.T) {
	t.Parallel()

	workflow := Named("step", func(ctx context.Context, t *CountingFlow) error {
		// Check if context is properly threaded
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	state := &CountingFlow{}
	trace, err := Traced(workflow)(ctx, state)

	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}

	// Trace should still be populated even if execution failed
	if len(trace.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(trace.Events))
	}

	if trace.TotalErrors != 1 {
		t.Errorf("expected 1 error, got %d", trace.TotalErrors)
	}
}

func TestFindEvent(t *testing.T) {
	t.Parallel()

	workflow := Do(
		Named("fast", func(ctx context.Context, t *CountingFlow) error {
			time.Sleep(1 * time.Millisecond)
			return nil
		}),
		Named("slow", func(ctx context.Context, t *CountingFlow) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		}),
		Named("error", func(ctx context.Context, t *CountingFlow) error {
			return fmt.Errorf("test error")
		}),
	)

	ctx := t.Context()
	state := &CountingFlow{}
	trace, _ := Traced(workflow)(ctx, state)

	testCases := []struct {
		name    string
		filters []TraceFilter
		want    string // expected step name, or "" if nil
	}{
		{
			name:    "find slow step",
			filters: []TraceFilter{MinDuration(10 * time.Millisecond)},
			want:    "slow",
		},
		{
			name:    "find error",
			filters: []TraceFilter{HasError()},
			want:    "error",
		},
		{
			name:    "find by name",
			filters: []TraceFilter{NameMatches("fast")},
			want:    "fast",
		},
		{
			name:    "no match",
			filters: []TraceFilter{MinDuration(1 * time.Hour)},
			want:    "",
		},
		{
			name: "multiple filters",
			filters: []TraceFilter{
				NoError(),
				MinDuration(10 * time.Millisecond),
			},
			want: "slow",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := trace.FindEvent(tc.filters...)

			if tc.want == "" {
				if event != nil {
					t.Errorf("expected nil, got event: %+v", event)
				}
			} else {
				if event == nil {
					t.Fatal("expected event, got nil")
				}
				if len(event.Names) == 0 || event.Names[len(event.Names)-1] != tc.want {
					t.Errorf("expected step %q, got %+v", tc.want, event.Names)
				}
			}
		})
	}
}

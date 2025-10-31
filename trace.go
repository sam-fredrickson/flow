// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// NamedError is an error returned by Named and related functions.
// It wraps the underlying error with the name of the step that failed.
// Users can use [errors.As] to detect and inspect NamedErrors.
type NamedError struct {
	// Name is the name of the step that failed.
	Name string
	// Err is the underlying error from the step.
	Err error
}

// Error returns the formatted error message.
func (e NamedError) Error() string {
	return fmt.Sprintf("%s: %v", e.Name, e.Err)
}

// Unwrap returns the underlying error.
func (e NamedError) Unwrap() error {
	return e.Err
}

// TraceEvent represents a single execution event in a traced workflow.
//
// Each event captures the full path of step names, start time, duration,
// and any error that occurred during execution.
type TraceEvent struct {
	// Names is the full hierarchical path of step names.
	// For example: ["parent", "child", "grandchild"]
	Names []string `json:"step_names"`

	// Start is when the step began execution.
	Start time.Time `json:"start"`

	// Duration is how long the step took to execute.
	Duration time.Duration `json:"duration"`

	// Error is the error message if the step failed, empty otherwise.
	Error string `json:"error,omitempty"`
}

// TraceOption configures trace behavior.
type TraceOption func(*traceOptions)

// traceOptions holds configuration for tracing.
type traceOptions struct {
	// StreamTo specifies where to write events as JSON Lines during execution.
	// Events are written as they complete, enabling real-time monitoring and
	// ensuring traces are preserved even if the process crashes.
	// All events are retained in memory for post-execution querying.
	// If nil, events are only stored in memory.
	StreamTo io.Writer
}

// WithStreamTo configures the trace to stream events as JSON Lines to the given writer.
//
// Events are written in JSON Lines format (one event per line) as they complete,
// enabling real-time monitoring and ensuring traces are preserved even if the
// process crashes. This is different from WriteTo/WriteJSONTo which output a
// pretty-printed JSON array after execution completes.
//
// All events are retained in memory for post-execution querying.
//
// Write failures to the stream are best-effort and do not cause the workflow to fail.
// This ensures that tracing infrastructure never breaks the workflow itself.
//
// Example:
//
//	f, _ := os.Create("trace.jsonl")
//	defer f.Close()
//	trace, err := flow.Traced(workflow, flow.WithStreamTo(f))(ctx, state)
//
//	// trace.jsonl contains one JSON object per line:
//	// {"step_names":["validate"],"start":"...","duration":45000000}
//	// {"step_names":["connect"],"start":"...","duration":120000000}
//	// ...
func WithStreamTo(w io.Writer) TraceOption {
	return func(opts *traceOptions) {
		opts.StreamTo = w
	}
}

// trace is the internal collection infrastructure used during workflow execution.
// It records events to the Trace result and handles streaming/context optimization.
type trace struct {
	mu       sync.Mutex
	streamTo io.Writer
	encoder  *json.Encoder
	result   *Trace
}

// Trace is the public result type containing execution events and metadata.
// All fields are directly accessible for querying and analysis.
type Trace struct {
	// Events is the list of all recorded trace events.
	Events []TraceEvent

	// Start is when the traced workflow began execution.
	Start time.Time

	// Duration is the total execution time of the workflow.
	// For filtered traces (from Filter), this is the sum of event durations.
	Duration time.Duration

	// TotalSteps is the total number of Named steps executed.
	// Only Named, NamedExtract, NamedTransform, NamedConsume, and AutoNamed
	// variants are counted. Unnamed steps are not included.
	// For filtered traces (from Filter), this equals len(Events).
	TotalSteps int

	// TotalErrors is the number of steps that failed with an error.
	// For filtered traces (from Filter), this is the count of events with errors.
	TotalErrors int
}

// eventIdx is a type-safe index into the trace's event array.
type eventIdx int

// Traced wraps a workflow and returns an Extract that produces a Trace.
//
// The trace records execution events for all Named steps within the workflow.
// This enables debugging, performance analysis, and understanding execution paths.
//
// Events are recorded in approximate start order but may have minor variations
// in parallel workflows due to mutex contention. For precise chronological ordering,
// sort events by their Start time.
//
// Options can be provided to configure streaming and context optimization.
// See [WithStreamTo].
//
// Example:
//
//	workflow := flow.Do(
//	    flow.Named("validate", ValidateConfig),
//	    flow.Named("connect", ConnectDB),
//	    flow.Named("migrate", RunMigrations),
//	)
//
//	// Basic tracing
//	err := flow.Spawn(
//	    flow.Traced(workflow),
//	    flow.WriteTextTo(os.Stdout),
//	)(ctx, state)
//
//	// Tracing with streaming to file
//	f, _ := os.Create("trace.jsonl")
//	defer f.Close()
//	trace, err := flow.Traced(workflow, flow.WithStreamTo(f))(ctx, state)
//
// Tracing is opt-in and has minimal overhead when not used.
// All Named, NamedExtract, NamedTransform, NamedConsume, and AutoNamed
// variants automatically record events when a trace is present in the context.
func Traced[T any](step Step[T], opts ...TraceOption) Extract[T, *Trace] {
	// Apply options with defaults
	options := traceOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	return func(ctx context.Context, t T) (result *Trace, err error) {
		result = &Trace{
			Start:  time.Now(),
			Events: make([]TraceEvent, 0),
		}

		tr := &trace{
			streamTo: options.StreamTo,
			result:   result,
		}

		// Initialize JSON encoder if streaming
		if tr.streamTo != nil {
			tr.encoder = json.NewEncoder(tr.streamTo)
		}

		f, _ := ctx.Value(flowCtxKey{}).(*flowCtx)
		f2 := newFlowCtx(ctx, f)
		f2.trace = tr

		ctx = f2
		func() {
			defer func() {
				result.Duration = time.Since(result.Start)

				// Flush buffered output if streaming
				if tr.streamTo != nil {
					if flusher, ok := tr.streamTo.(interface{ Flush() error }); ok {
						_ = flusher.Flush() // Best-effort
					}
				}
			}()
			err = step(ctx, t)
		}()

		return result, err
	}
}

// getTrace retrieves the trace from the context, or nil if not present.
func getTrace(ctx context.Context) *trace {
	f, ok := ctx.Value(flowCtxKey{}).(*flowCtx)
	if !ok {
		return nil
	}
	return f.trace
}

// newEvent creates a new trace event and returns its index.
//
// This should be called at the start of step execution. The returned
// index must be passed to recordFinish when the step completes.
func (t *trace) newEvent(names []string) eventIdx {
	t.mu.Lock()
	defer t.mu.Unlock()

	idx := len(t.result.Events)
	t.result.Events = append(t.result.Events, TraceEvent{
		Names: names,
		Start: time.Now(),
	})
	t.result.TotalSteps++

	return eventIdx(idx)
}

// recordFinish updates an event with its duration and error (if any).
//
// This should be called when a step completes execution.
func (t *trace) recordFinish(idx eventIdx, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	event := &t.result.Events[idx]
	event.Duration = time.Since(event.Start)
	if err != nil {
		// Unwrap only through NamedErrors to avoid stripping external library wrappers.
		// This preserves context from external libraries while removing redundant
		// error messages from our own wrapping chain.
		recordErr := err
		for {
			var namedErr NamedError
			if errors.As(recordErr, &namedErr) && namedErr.Err != nil {
				recordErr = namedErr.Err
			} else {
				break
			}
		}
		event.Error = recordErr.Error()
		t.result.TotalErrors++
	}

	// Stream event if enabled (best-effort)
	if t.streamTo != nil {
		_ = t.encoder.Encode(event)
	}
}

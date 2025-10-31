// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteTo serializes the trace as JSON to the given writer.
//
// Returns the number of bytes written and any error.
// The JSON is formatted as a pretty-printed array of events.
//
// This is different from streaming (via WithStreamTo) which outputs JSON Lines
// format (one event per line) during execution. WriteTo outputs a single JSON
// array after execution completes.
func (t *Trace) WriteTo(w io.Writer) (int64, error) {
	// Pretty-print JSON
	data, err := json.MarshalIndent(t.Events, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal trace: %w", err)
	}

	n, err := w.Write(data)
	if err != nil {
		return int64(n), fmt.Errorf("failed to write trace: %w", err)
	}

	// Add newline for better terminal output
	if nn, err := w.Write([]byte("\n")); err != nil {
		return int64(n + nn), fmt.Errorf("failed to write newline: %w", err)
	} else {
		n += nn
	}

	return int64(n), nil
}

// WriteText outputs a human-readable tree view of the trace.
//
// The tree view shows the hierarchical structure of steps with their
// durations. Indentation reflects nesting depth, and the displayed
// name is the last element of the step path.
//
// Example output:
//
//	validate (45ms)
//	connect (120ms)
//	migrate (2.3s)
//	  create-tables (1.8s)
//	  create-indexes (500ms)
//
// For parallel workflows where events may be interleaved, the tree view
// shows execution order, not logical nesting, which can be misleading.
// For parallel workflows, use [WriteFlatText] instead, which shows full
// paths and makes event relationships clearer.
func (t *Trace) WriteText(w io.Writer) (int64, error) {
	var totalBytes int64
	for _, event := range t.Events {
		// Indentation based on depth
		depth := len(event.Names)
		if depth > 0 {
			depth-- // Root level has no indent
		}
		indent := strings.Repeat("  ", depth)

		// Get the step name (last element of path)
		name := "<unknown>"
		if len(event.Names) > 0 {
			name = event.Names[len(event.Names)-1]
		}

		// Format duration
		duration := event.Duration.String()

		// Format line
		line := fmt.Sprintf("%s%s (%s)", indent, name, duration)
		if event.Error != "" {
			line += fmt.Sprintf(" [ERROR: %s]", event.Error)
		}
		line += "\n"

		n, err := w.Write([]byte(line))
		totalBytes += int64(n)
		if err != nil {
			return totalBytes, fmt.Errorf("failed to write text: %w", err)
		}
	}

	return totalBytes, nil
}

// WriteFlatText outputs a human-readable flat list of events.
//
// Unlike [WriteText], this method shows events in a chronological list with
// full paths (e.g., "parent > child") and no tree indentation. This format
// is recommended for analyzing parallel workflows where tree structure may
// be misleading due to event interleaving.
//
// Example output:
//
//	validate (45ms)
//	connect (120ms)
//	migrate (2.3s)
//	migrate > create-tables (1.8s)
//	migrate > create-indexes (500ms)
//
// Events appear in the order they were recorded (approximate start order).
// For precise chronological ordering, sort by Start time before formatting:
//
//	events := trace.Events
//	sort.Slice(events, func(i, j int) bool {
//	    return events[i].Start.Before(events[j].Start)
//	})
func (t *Trace) WriteFlatText(w io.Writer) (int64, error) {
	var totalBytes int64
	for _, event := range t.Events {
		// Build full path
		path := "<unknown>"
		if len(event.Names) > 0 {
			path = strings.Join(event.Names, " > ")
		}

		// Format duration
		duration := event.Duration.String()

		// Format line
		line := fmt.Sprintf("%s (%s)", path, duration)
		if event.Error != "" {
			line += fmt.Sprintf(" [ERROR: %s]", event.Error)
		}
		line += "\n"

		n, err := w.Write([]byte(line))
		totalBytes += int64(n)
		if err != nil {
			return totalBytes, fmt.Errorf("failed to write flat text: %w", err)
		}
	}

	return totalBytes, nil
}

// WriteJSONTo returns a Step that serializes a trace to JSON.
//
// This enables natural composition with Spawn:
//
//	workflow := flow.Spawn(
//	    flow.Traced(myWorkflow),
//	    flow.WriteJSONTo(os.Stdout),
//	)
func WriteJSONTo(w io.Writer) Step[*Trace] {
	return func(ctx context.Context, trace *Trace) error {
		_, err := trace.WriteTo(w)
		return err
	}
}

// WriteTextTo returns a Step that serializes a trace to human-readable text.
//
// This enables natural composition with Spawn:
//
//	workflow := flow.Spawn(
//	    flow.Traced(myWorkflow),
//	    flow.WriteTextTo(os.Stdout),
//	)
func WriteTextTo(w io.Writer) Step[*Trace] {
	return func(ctx context.Context, trace *Trace) error {
		_, err := trace.WriteText(w)
		return err
	}
}

// WriteFlatTextTo returns a Step that serializes a trace to flat text format.
//
// This enables natural composition with Spawn. The flat format shows events
// in chronological order without tree indentation, making it more suitable
// for analyzing parallel workflows.
//
//	workflow := flow.Spawn(
//	    flow.Traced(myWorkflow),
//	    flow.WriteFlatTextTo(os.Stdout),
//	)
func WriteFlatTextTo(w io.Writer) Step[*Trace] {
	return func(ctx context.Context, trace *Trace) error {
		_, err := trace.WriteFlatText(w)
		return err
	}
}

// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"path/filepath"
	"strings"
	"time"
)

// TraceFilter is a predicate function for filtering trace events.
type TraceFilter func(TraceEvent) bool

// FindEvent returns the first event matching all provided filters, or nil if none match.
//
// Multiple filters are AND'd together.
//
// Example:
//
//	// Find the first slow database operation
//	event := trace.FindEvent(
//	    flow.PathMatches("database.*"),
//	    flow.MinDuration(time.Second),
//	)
//	if event != nil {
//	    log.Printf("slow DB op: %s took %v", event.Names, event.Duration)
//	}
func (t *Trace) FindEvent(filters ...TraceFilter) *TraceEvent {
	for i := range t.Events {
		event := &t.Events[i]
		match := true
		for _, filter := range filters {
			if !filter(*event) {
				match = false
				break
			}
		}
		if match {
			return event
		}
	}
	return nil
}

// Filter returns a new Trace containing only events matching all provided filters.
//
// Multiple filters are AND'd together. The original trace is not modified.
//
// The returned trace's TotalSteps equals the number of filtered events,
// and TotalErrors equals the number of filtered events with errors.
// The Duration field represents the sum of durations of all filtered events.
// The Start field is set to the earliest start time of the filtered events.
// If no events match, Start is set to the original trace's Start time.
//
// Example:
//
//	// Find slow steps with errors
//	filtered := trace.Filter(
//	    flow.MinDuration(time.Second),
//	    flow.HasError(),
//	)
func (t *Trace) Filter(filters ...TraceFilter) *Trace {
	// Pre-allocate with full capacity (optimistic: many events will match)
	// This reduces allocations when filtering produces similar-sized results
	filtered := make([]TraceEvent, 0, len(t.Events))
	errorCount := 0
	var totalDuration time.Duration
	var earliestStart time.Time

	for _, event := range t.Events {
		match := true
		for _, filter := range filters {
			if !filter(event) {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, event)
			totalDuration += event.Duration
			if event.Error != "" {
				errorCount++
			}
			// Track earliest start time
			if earliestStart.IsZero() || event.Start.Before(earliestStart) {
				earliestStart = event.Start
			}
		}
	}

	// If no events matched, use original trace start time
	startTime := t.Start
	if !earliestStart.IsZero() {
		startTime = earliestStart
	}

	return &Trace{
		Events:      filtered,
		Start:       startTime,
		Duration:    totalDuration,
		TotalSteps:  len(filtered),
		TotalErrors: errorCount,
	}
}

// MinDuration returns a filter that matches events with duration >= d.
func MinDuration(d time.Duration) TraceFilter {
	return func(event TraceEvent) bool {
		return event.Duration >= d
	}
}

// MaxDuration returns a filter that matches events with duration <= d.
func MaxDuration(d time.Duration) TraceFilter {
	return func(event TraceEvent) bool {
		return event.Duration <= d
	}
}

// HasError returns a filter that matches events with errors.
func HasError() TraceFilter {
	return func(event TraceEvent) bool {
		return event.Error != ""
	}
}

// NoError returns a filter that matches events without errors.
func NoError() TraceFilter {
	return func(event TraceEvent) bool {
		return event.Error == ""
	}
}

// NameMatches returns a filter that matches events where the step name
// (last element of Names) matches the glob pattern.
//
// Patterns use filepath.Match semantics—see its documentation for the full details
// of matching behavior (e.g., *, ?, and [...] for character sets).
//
// If the pattern is malformed, no events will match (returns false).
func NameMatches(pattern string) TraceFilter {
	return func(event TraceEvent) bool {
		if len(event.Names) == 0 {
			return false
		}
		name := event.Names[len(event.Names)-1]
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			// Invalid pattern - fail closed (no matches)
			return false
		}
		return matched
	}
}

// NamePrefix returns a filter that matches events where the step name
// (last element of Names) has the given prefix.
func NamePrefix(prefix string) TraceFilter {
	return func(event TraceEvent) bool {
		if len(event.Names) == 0 {
			return false
		}
		name := event.Names[len(event.Names)-1]
		return strings.HasPrefix(name, prefix)
	}
}

// PathMatches returns a filter that matches events where the full
// dotted path matches the glob pattern.
//
// Patterns use filepath.Match semantics—see its documentation for the full details
// of matching behavior. The path is constructed by joining Names with "." separators.
//
// If the pattern is malformed, no events will match (returns false).
func PathMatches(pattern string) TraceFilter {
	return func(event TraceEvent) bool {
		if len(event.Names) == 0 {
			return false
		}
		path := strings.Join(event.Names, ".")
		matched, err := filepath.Match(pattern, path)
		if err != nil {
			// Invalid pattern - fail closed (no matches)
			return false
		}
		return matched
	}
}

// HasPathPrefix returns a filter that matches events where the full
// Names path has the given prefix.
//
// Example:
//
//	// Match all steps under ["migrate", "tables"]
//	filter := flow.HasPathPrefix([]string{"migrate", "tables"})
func HasPathPrefix(prefix []string) TraceFilter {
	return func(event TraceEvent) bool {
		if len(event.Names) < len(prefix) {
			return false
		}
		for i, p := range prefix {
			if event.Names[i] != p {
				return false
			}
		}
		return true
	}
}

// DepthEquals returns a filter that matches events at the given depth.
//
// Depth is defined as len(Names). For example:
//   - depth 1: top-level steps
//   - depth 2: first level of nesting
//   - depth 3: second level of nesting
func DepthEquals(depth int) TraceFilter {
	return func(event TraceEvent) bool {
		return len(event.Names) == depth
	}
}

// DepthAtMost returns a filter that matches events at or above the given depth.
//
// For example, DepthAtMost(2) matches steps at depth 1 or 2.
func DepthAtMost(depth int) TraceFilter {
	return func(event TraceEvent) bool {
		return len(event.Names) <= depth
	}
}

// TimeRange returns a filter that matches events that started within the given time range.
//
// Both start and end times are inclusive. Events are matched if their Start time
// falls between start and end.
//
// Example:
//
//	// Find events that started in the first second
//	workflowStart := trace.Events()[0].Start
//	filter := flow.TimeRange(workflowStart, workflowStart.Add(time.Second))
func TimeRange(start, end time.Time) TraceFilter {
	return func(event TraceEvent) bool {
		return !event.Start.Before(start) && !event.Start.After(end)
	}
}

// ErrorMatches returns a filter that matches events where the error message
// matches the glob pattern.
//
// If the pattern is malformed, no events will match (returns false).
//
// Example patterns:
//   - "*timeout*" matches errors containing "timeout"
//   - "connection *" matches errors starting with "connection "
//   - "database error" matches exactly "database error"
func ErrorMatches(pattern string) TraceFilter {
	return func(event TraceEvent) bool {
		if event.Error == "" {
			return false
		}
		matched, err := filepath.Match(pattern, event.Error)
		if err != nil {
			// Invalid pattern - fail closed (no matches)
			return false
		}
		return matched
	}
}

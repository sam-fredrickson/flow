// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTraceOutputFormats(t *testing.T) {
	t.Parallel()

	workflow := Named("parent", Do(
		Named("child1", func(ctx context.Context, t *CountingFlow) error {
			return nil
		}),
		Named("child2", func(ctx context.Context, t *CountingFlow) error {
			return errors.New("test error")
		}),
	))

	ctx := t.Context()
	state := &CountingFlow{}
	trace, _ := Traced(workflow)(ctx, state)

	testCases := []struct {
		name      string
		writeFunc func(*testing.T, *Trace, *bytes.Buffer)
		checkFunc func(*testing.T, *bytes.Buffer)
	}{
		{
			name: "WriteTo JSON",
			writeFunc: func(t *testing.T, trace *Trace, buf *bytes.Buffer) {
				n, err := trace.WriteTo(buf)
				if err != nil {
					t.Fatalf("WriteTo failed: %v", err)
				}
				if n == 0 {
					t.Error("expected non-zero bytes written")
				}
			},
			checkFunc: func(t *testing.T, buf *bytes.Buffer) {
				var events []TraceEvent
				if err := json.Unmarshal(buf.Bytes(), &events); err != nil {
					t.Fatalf("failed to parse JSON: %v", err)
				}
				if len(events) != 3 {
					t.Errorf("expected 3 events, got %d", len(events))
				}
			},
		},
		{
			name: "WriteText",
			writeFunc: func(t *testing.T, trace *Trace, buf *bytes.Buffer) {
				n, err := trace.WriteText(buf)
				if err != nil {
					t.Fatalf("WriteText failed: %v", err)
				}
				if n == 0 {
					t.Error("expected non-zero bytes written")
				}
			},
			checkFunc: func(t *testing.T, buf *bytes.Buffer) {
				output := buf.String()
				expected := []string{"parent", "child1", "child2", "ERROR"}
				for _, exp := range expected {
					if !strings.Contains(output, exp) {
						t.Errorf("expected output to contain %q", exp)
					}
				}
			},
		},
		{
			name: "WriteFlatText",
			writeFunc: func(t *testing.T, trace *Trace, buf *bytes.Buffer) {
				n, err := trace.WriteFlatText(buf)
				if err != nil {
					t.Fatalf("WriteFlatText failed: %v", err)
				}
				if n == 0 {
					t.Error("expected non-zero bytes written")
				}
			},
			checkFunc: func(t *testing.T, buf *bytes.Buffer) {
				output := buf.String()
				expectedPaths := []string{"parent", "parent > child1", "parent > child2"}
				for _, path := range expectedPaths {
					if !strings.Contains(output, path) {
						t.Errorf("expected output to contain path %q", path)
					}
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			tc.writeFunc(t, trace, &buf)
			tc.checkFunc(t, &buf)
		})
	}

	t.Run("WriteJSONTo step", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		step := WriteJSONTo(&buf)
		ctx := t.Context()
		if err := step(ctx, trace); err != nil {
			t.Fatalf("WriteJSONTo step failed: %v", err)
		}
		var events []TraceEvent
		if err := json.Unmarshal(buf.Bytes(), &events); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}
	})

	t.Run("WriteTextTo step", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		step := WriteTextTo(&buf)
		ctx := t.Context()
		if err := step(ctx, trace); err != nil {
			t.Fatalf("WriteTextTo step failed: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected non-zero output")
		}
	})

	t.Run("WriteFlatTextTo step", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		step := WriteFlatTextTo(&buf)
		ctx := t.Context()
		if err := step(ctx, trace); err != nil {
			t.Fatalf("WriteFlatTextTo step failed: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected non-zero output")
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		step     Step[*CountingFlow]
		expected []string
	}{
		{
			name: "NoNames",
			step: func(ctx context.Context, c *CountingFlow) error {
				names := Names(ctx)
				if names != nil {
					t.Errorf("expected nil, got %v", names)
				}
				return nil
			},
			expected: nil,
		},
		{
			name: "SingleName",
			step: Named("outer", func(ctx context.Context, c *CountingFlow) error {
				names := Names(ctx)
				if len(names) != 1 || names[0] != "outer" {
					t.Errorf("expected [outer], got %v", names)
				}
				return nil
			}),
			expected: []string{"outer"},
		},
		{
			name: "NestedNames",
			step: Named("outer", Named("middle", Named("inner", func(ctx context.Context, c *CountingFlow) error {
				names := Names(ctx)
				if len(names) != 3 || names[0] != "outer" || names[1] != "middle" || names[2] != "inner" {
					t.Errorf("expected [outer middle inner], got %v", names)
				}
				return nil
			}))),
			expected: []string{"outer", "middle", "inner"},
		},
		{
			name: "NamesAreImmutable",
			step: Named("outer", func(ctx context.Context, c *CountingFlow) error {
				names1 := Names(ctx)
				names1[0] = "modified"
				names2 := Names(ctx)
				if names2[0] != "outer" {
					t.Errorf("expected [outer], got %v - Names should return a copy", names2)
				}
				return nil
			}),
			expected: []string{"outer"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c CountingFlow
			err := tc.step(t.Context(), &c)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLogger(t *testing.T) {
	t.Parallel()

	t.Run("ReturnsDefaultWhenNotSet", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := Logger(ctx)
		if logger != log.Default() {
			t.Errorf("expected log.Default(), got different logger")
		}
	})

	t.Run("ReturnsConfiguredLogger", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		customLogger := log.New(&buf, "test: ", 0)

		step := WithLogger(customLogger, func(ctx context.Context, c *CountingFlow) error {
			logger := Logger(ctx)
			if logger != customLogger {
				t.Errorf("expected custom logger, got different logger")
			}
			return nil
		})

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestSlogger(t *testing.T) {
	t.Parallel()

	t.Run("ReturnsDefaultWhenNotSet", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := Slogger(ctx)
		if logger != slog.Default() {
			t.Errorf("expected slog.Default(), got different logger")
		}
	})

	t.Run("ReturnsConfiguredLogger", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		customLogger := slog.New(slog.NewTextHandler(&buf, nil))

		step := WithSlogger(customLogger, func(ctx context.Context, c *CountingFlow) error {
			logger := Slogger(ctx)
			if logger != customLogger {
				t.Errorf("expected custom logger, got different logger")
			}
			return nil
		})

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestWithLogging(t *testing.T) {
	t.Parallel()

	t.Run("LogsUnknownWhenNoName", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		logger := log.New(&buf, "", 0)

		step := WithLogger(logger, WithLogging(Increment(1)))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "[<unknown>] starting step") {
			t.Errorf("expected log to contain '[<unknown>] starting step', got: %s", output)
		}
		if !strings.Contains(output, "[<unknown>] finished step") {
			t.Errorf("expected log to contain '[<unknown>] finished step', got: %s", output)
		}
	})

	t.Run("LogsSingleName", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		logger := log.New(&buf, "", 0)

		step := WithLogger(logger,
			Named("test",
				WithLogging(Increment(1))))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "[test] starting step") {
			t.Errorf("expected log to contain '[test] starting step', got: %s", output)
		}
		if !strings.Contains(output, "[test] finished step") {
			t.Errorf("expected log to contain '[test] finished step', got: %s", output)
		}
	})

	t.Run("LogsNestedNames", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		logger := log.New(&buf, "", 0)

		step := WithLogger(logger,
			Named("outer",
				WithLogging(
					Named("inner",
						WithLogging(Increment(1))))))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "[outer] starting step") {
			t.Errorf("expected log to contain '[outer] starting step', got: %s", output)
		}
		if !strings.Contains(output, "[outer.inner] starting step") {
			t.Errorf("expected log to contain '[outer.inner] starting step', got: %s", output)
		}
		if !strings.Contains(output, "[outer.inner] finished step") {
			t.Errorf("expected log to contain '[outer.inner] finished step', got: %s", output)
		}
		if !strings.Contains(output, "[outer] finished step") {
			t.Errorf("expected log to contain '[outer] finished step', got: %s", output)
		}
	})

	t.Run("UsesDefaultLoggerWhenNotConfigured", func(t *testing.T) {
		t.Parallel()
		// Use a discarding logger to avoid polluting test output
		logger := log.New(io.Discard, "", 0)
		step := WithLogger(logger, Named("test", WithLogging(Increment(1))))
		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestWithSlogging(t *testing.T) {
	t.Parallel()

	t.Run("LogsUnknownWhenNoName", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		step := WithSlogger(logger, WithSlogging(slog.LevelInfo, Increment(1)))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\n")

		// Parse first log entry
		var log1 map[string]interface{}
		if err := json.Unmarshal([]byte(lines[0]), &log1); err != nil {
			t.Fatalf("failed to parse first log line: %v", err)
		}
		if log1["msg"] != "starting step" || log1["name"] != "<unknown>" {
			t.Errorf("expected starting step with name=<unknown>, got: %v", log1)
		}

		// Parse second log entry
		var log2 map[string]interface{}
		if err := json.Unmarshal([]byte(lines[1]), &log2); err != nil {
			t.Fatalf("failed to parse second log line: %v", err)
		}
		if log2["msg"] != "finished step" || log2["name"] != "<unknown>" {
			t.Errorf("expected finished step with name=<unknown>, got: %v", log2)
		}
	})

	t.Run("LogsSingleName", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		step := WithSlogger(logger,
			Named("test",
				WithSlogging(slog.LevelInfo, Increment(1))))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\n")

		var log1 map[string]interface{}
		if err := json.Unmarshal([]byte(lines[0]), &log1); err != nil {
			t.Fatalf("failed to parse first log line: %v", err)
		}
		if log1["msg"] != "starting step" || log1["name"] != "test" {
			t.Errorf("expected starting step with name=test, got: %v", log1)
		}
	})

	t.Run("LogsNestedNames", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		// Set level to Debug to ensure all logs are captured
		logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		step := WithSlogger(logger,
			Named("outer",
				WithSlogging(slog.LevelInfo,
					Named("inner",
						WithSlogging(slog.LevelDebug, Increment(1))))))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\n")

		// We should have 4 log lines: outer start, outer.inner start, outer.inner finish, outer finish
		if len(lines) != 4 {
			t.Fatalf("expected 4 log lines, got %d: %s", len(lines), output)
		}

		// Check outer starting
		var log1 map[string]interface{}
		if err := json.Unmarshal([]byte(lines[0]), &log1); err != nil {
			t.Fatalf("failed to parse log line 1: %v", err)
		}
		if log1["msg"] != "starting step" || log1["name"] != "outer" {
			t.Errorf("expected starting step with name=outer, got: %v", log1)
		}

		// Check outer.inner starting
		var log2 map[string]interface{}
		if err := json.Unmarshal([]byte(lines[1]), &log2); err != nil {
			t.Fatalf("failed to parse log line 2: %v", err)
		}
		if log2["msg"] != "starting step" || log2["name"] != "outer.inner" {
			t.Errorf("expected starting step with name=outer.inner, got: %v", log2)
		}

		// Check outer.inner finished
		var log3 map[string]interface{}
		if err := json.Unmarshal([]byte(lines[2]), &log3); err != nil {
			t.Fatalf("failed to parse log line 3: %v", err)
		}
		if log3["msg"] != "finished step" || log3["name"] != "outer.inner" {
			t.Errorf("expected finished step with name=outer.inner, got: %v", log3)
		}

		// Check outer finished
		var log4 map[string]interface{}
		if err := json.Unmarshal([]byte(lines[3]), &log4); err != nil {
			t.Fatalf("failed to parse log line 4: %v", err)
		}
		if log4["msg"] != "finished step" || log4["name"] != "outer" {
			t.Errorf("expected finished step with name=outer, got: %v", log4)
		}
	})

	t.Run("UsesDefaultLoggerWhenNotConfigured", func(t *testing.T) {
		t.Parallel()
		// Use a discarding logger to avoid polluting test output
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		step := WithSlogger(logger, Named("test", WithSlogging(slog.LevelInfo, Increment(1))))
		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

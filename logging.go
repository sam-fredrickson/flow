// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"log"
	"log/slog"
	"strings"
	"time"
)

// Logger returns the [log.Logger] from the context, or [log.Default] if none is set.
//
// This is useful for custom logging decorators that need to access the configured logger.
//
// Example:
//
//	func CustomLogging[T any](step flow.Step[T]) flow.Step[T] {
//	    return func(ctx context.Context, t T) error {
//	        logger := flow.Logger(ctx)
//	        names := flow.Names(ctx)
//	        // Custom logging logic here
//	        logger.Printf("Custom: %v\n", names)
//	        return step(ctx, t)
//	    }
//	}
func Logger(ctx context.Context) *log.Logger {
	f, ok := ctx.Value(flowCtxKey{}).(*flowCtx)
	if !ok {
		return log.Default()
	}
	return f.logger
}

// WithLogger configures a [Step] to use a specific [log.Logger] for logging.
//
// The logger is stored in the context and used by [WithLogging]. If no logger
// is configured, [WithLogging] will use [log.Default].
//
// This is typically applied once at the root of a workflow to configure logging
// for all nested steps. For optimal performance, use [Named] or [AutoNamed]
// liberally throughout your workflow—the flow library consolidates all context
// state into a single lookup via flowCtx, but this optimization only provides
// constant-time access when step names are present in the context chain.
// Using [WithLogger] without corresponding [Named] steps may incur linear
// context chain traversal cost.
//
// Example:
//
//	myLogger := log.New(os.Stdout, "workflow: ", log.LstdFlags)
//	step := WithLogger(myLogger,
//	    Named("process",
//	        WithLogging(processStep)))
func WithLogger[T any](logger *log.Logger, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		f, _ := ctx.Value(flowCtxKey{}).(*flowCtx)
		f2 := newFlowCtx(ctx, f)
		f2.logger = logger
		return step(f2, t)
	}
}

// WithLogging wraps a [Step] with logging that prints messages when the step
// starts and finishes, including execution duration.
//
// The log messages include the full dotted path of step names from the context
// (e.g., "process.parse.validate"). If no names are set, logs show "<unknown>".
//
// The logger used is retrieved from the context (set by [WithLogger]). If no
// logger is configured, [log.Default] is used.
//
// Log format:
//
//	[step.name] starting step
//	[step.name] finished step (took 123ms)
//
// Example:
//
//	step := Named("process",
//	    WithLogging(
//	        Named("parse",
//	            WithLogging(parseStep))))
//
// This would log:
//
//	[process] starting step
//	[process.parse] starting step
//	[process.parse] finished step (took 5ms)
//	[process] finished step (took 10ms)
//
// For custom logging formats, use [Names] to retrieve the name stack
// and implement your own logging decorator.
func WithLogging[T any](step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		names := Names(ctx)
		var fullName string
		if len(names) == 0 {
			fullName = "<unknown>"
		} else {
			fullName = strings.Join(names, ".")
		}

		logger := Logger(ctx)

		logger.Printf("[%s] starting step\n", fullName)
		start := time.Now()
		err := step(ctx, t)
		duration := time.Since(start)
		logger.Printf("[%s] finished step (took %v)\n", fullName, duration)
		return err
	}
}

// Slogger returns the [slog.Logger] from the context, or [slog.Default] if none is set.
//
// This is useful for custom logging decorators that need to access the configured
// structured logger.
//
// Example:
//
//	func CustomSlogging[T any](step flow.Step[T]) flow.Step[T] {
//	    return func(ctx context.Context, t T) error {
//	        logger := flow.Slogger(ctx)
//	        names := flow.Names(ctx)
//	        // Custom logging logic here
//	        logger.Info("custom step", "names", names)
//	        return step(ctx, t)
//	    }
//	}
func Slogger(ctx context.Context) *slog.Logger {
	f, ok := ctx.Value(flowCtxKey{}).(*flowCtx)
	if !ok {
		return slog.Default()
	}
	return f.slogger
}

// WithSlogger configures a [Step] to use a specific [slog.Logger] for structured logging.
//
// The logger is stored in the context and used by [WithSlogging]. If no logger
// is configured, [WithSlogging] will use [slog.Default].
//
// This is typically applied once at the root of a workflow to configure logging
// for all nested steps. For optimal performance, use [Named] or [AutoNamed]
// liberally throughout your workflow—the flow library consolidates all context
// state into a single lookup via flowCtx, but this optimization only provides
// constant-time access when step names are present in the context chain.
// Using [WithSlogger] without corresponding [Named] steps may incur linear
// context chain traversal cost.
//
// Example:
//
//	myLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
//	step := WithSlogger(myLogger,
//	    Named("process",
//	        WithSlogging(slog.LevelInfo, processStep)))
func WithSlogger[T any](logger *slog.Logger, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		f, _ := ctx.Value(flowCtxKey{}).(*flowCtx)
		f2 := newFlowCtx(ctx, f)
		f2.slogger = logger
		return step(f2, t)
	}
}

// WithSlogging wraps a [Step] with structured logging that emits log records
// when the step starts and finishes, including execution duration.
//
// The log records include the full dotted path of step names from the context
// as a "name" attribute (e.g., "process.parse.validate"). If no names are set,
// the name attribute will be "<unknown>". The finish log includes a "duration_ms"
// attribute with the execution time in milliseconds.
//
// The logger used is retrieved from the context (set by [WithSlogger]). If no
// logger is configured, [slog.Default] is used.
//
// Example:
//
//	step := Named("process",
//	    WithSlogging(slog.LevelInfo,
//	        Named("parse",
//	            WithSlogging(slog.LevelDebug, parseStep))))
//
// This would emit structured log records similar to:
//
//	{"level":"info","msg":"starting step","name":"process"}
//	{"level":"debug","msg":"starting step","name":"process.parse"}
//	{"level":"debug","msg":"finished step","name":"process.parse","duration_ms":5}
//	{"level":"info","msg":"finished step","name":"process","duration_ms":10}
//
// For custom logging formats, use [Names] to retrieve the name stack
// and implement your own logging decorator.
func WithSlogging[T any](level slog.Level, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		names := Names(ctx)
		var fullName string
		if len(names) == 0 {
			fullName = "<unknown>"
		} else {
			fullName = strings.Join(names, ".")
		}

		logger := Slogger(ctx)

		logger.Log(ctx, level, "starting step", "name", fullName)
		start := time.Now()
		err := step(ctx, t)
		duration := time.Since(start)
		logger.Log(ctx, level, "finished step", "name", fullName, "duration_ms", duration.Milliseconds())
		return err
	}
}

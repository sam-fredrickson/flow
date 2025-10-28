// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

type stepNameKey struct{}

// StepNames returns a copy of the step name stack from the context.
// Returns nil if no names are present in the context.
//
// This is useful for custom logging decorators or other functionality
// that needs to inspect the current step hierarchy.
func StepNames(ctx context.Context) []string {
	names, ok := ctx.Value(stepNameKey{}).([]string)
	if !ok || len(names) == 0 {
		return nil
	}
	// Return a copy to prevent mutation
	return append([]string{}, names...)
}

// Named wraps a [Step] with a name.
//
// The name is prepended to the error message of the [Step], separated by a colon.
// For example, if the [Step] returns an error "invalid config", the name is
// prepended to the error message: "example: invalid config".
//
// Named also maintains a stack of step names in the context, which can be
// retrieved using [StepNames]. When Named decorators are nested, each appends
// its name to the stack, creating a hierarchical path (e.g., "process.parse.validate").
//
// This is useful for debugging and logging.
func Named[T any](name string, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		// Get existing names and append this one
		names, _ := ctx.Value(stepNameKey{}).([]string)
		newNames := append(append([]string{}, names...), name)
		ctx = context.WithValue(ctx, stepNameKey{}, newNames)

		if err := step(ctx, t); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
}

// NamedTransform wraps a [Transform] with a name.
//
// The name is prepended to the error message of the [Transform], separated by a
// colon. For example, if the [Transform] returns an error "invalid config", the
// name is prepended to the error message: "example: invalid config".
//
// This is useful for debugging and logging.
func NamedTransform[T any, In any, Out any](
	name string,
	transform Transform[T, In, Out],
) Transform[T, In, Out] {
	return func(ctx context.Context, t T, in In) (Out, error) {
		out, err := transform(ctx, t, in)
		if err != nil {
			return out, fmt.Errorf("%s: %w", name, err)
		}
		return out, nil
	}
}

// NamedExtract wraps an [Extract] with a name.
//
// The name is prepended to the error message of the [Extract], separated by a
// colon. For example, if the [Extract] returns an error "invalid config", the
// name is prepended to the error message: "example: invalid config".
//
// This is useful for debugging and logging.
func NamedExtract[T any, U any](
	name string,
	extract Extract[T, U],
) Extract[T, U] {
	return func(ctx context.Context, t T) (U, error) {
		u, err := extract(ctx, t)
		if err != nil {
			return u, fmt.Errorf("%s: %w", name, err)
		}
		return u, nil
	}
}

// NamedConsume wraps a [Consume] with a name.
//
// The name is prepended to the error message of the [Consume], separated by a
// colon. For example, if the [Consume] returns an error "invalid config", the
// name is prepended to the error message: "example: invalid config".
//
// This is useful for debugging and logging.
func NamedConsume[T any, U any](
	name string,
	consume Consume[T, U],
) Consume[T, U] {
	return func(ctx context.Context, t T, u U) error {
		if err := consume(ctx, t, u); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
}

type autoNamedOptions struct {
	callerSkip int
}

// An AutoNamedOption is a function option for [AutoNamed] and related functions.
type AutoNamedOption func(*autoNamedOptions)

// SkipCaller adds a delta to the number of skipped stack frames.
//
// This is useful when wrapping decorators inside wrapper functions, allowing
// AutoNamed to skip intermediate layers and identify the original caller.
//
// Example:
//
//	func SomeStep() flow.Step[*Task] {
//	    return TaskStep(func(ctx context.Context, task *Task) error {
//	        // Implementation
//	        return nil
//	    })
//	}
//
//	func TaskStep(step flow.Step[*Task]) flow.Step[*Task] {
//	    // Skip TaskStep so AutoNamed picks SomeStep instead
//	    return AutoNamed(Retry(step), flow.SkipCaller(1))
//	}
func SkipCaller(delta int) AutoNamedOption {
	return func(o *autoNamedOptions) {
		o.callerSkip += delta
	}
}

// AutoNamed wraps a [Step] with a name automatically derived from the calling function.
//
// This uses reflection to extract the name of the function that calls AutoNamed,
// which is useful for reducing repetition when naming steps in step constructors.
//
// Example:
//
//	func CreateDatabase() flow.Step[*Config] {
//	    return flow.AutoNamed(func(ctx context.Context, cfg *Config) error {
//	        // Implementation
//	        return nil
//	    })
//	}
//	// If this step fails, the error will be prefixed with "CreateDatabase: ..."
//
// Note: AutoNamed only works when called directly from a named function.
// It will not work correctly when called from anonymous functions or closures.
func AutoNamed[T any](step Step[T], opts ...AutoNamedOption) Step[T] {
	return autoNamed(step, Named, opts...)
}

// AutoNamedTransform wraps a [Transform] with a name automatically derived from the calling function.
//
// See AutoNamed for further details.
func AutoNamedTransform[T, A, B any](transform Transform[T, A, B], opts ...AutoNamedOption) Transform[T, A, B] {
	return autoNamed(transform, NamedTransform, opts...)
}

// AutoNamedExtract wraps an [Extract] with a name automatically derived from the calling function.
//
// See AutoNamed for further details.
func AutoNamedExtract[T, U any](extract Extract[T, U], opts ...AutoNamedOption) Extract[T, U] {
	return autoNamed(extract, NamedExtract, opts...)
}

// AutoNamedConsume wraps a [Consume] with a name automatically derived from the calling function.
//
// See AutoNamed for further details.
func AutoNamedConsume[T, U any](consume Consume[T, U], opts ...AutoNamedOption) Consume[T, U] {
	return autoNamed(consume, NamedConsume, opts...)
}

func autoNamed[T any](thing T, namer func(string, T) T, opts ...AutoNamedOption) T {
	const minimumCallerSkip = 2
	config := autoNamedOptions{callerSkip: minimumCallerSkip}
	for _, opt := range opts {
		opt(&config)
	}

	// Get the calling function name
	pc, _, _, ok := runtime.Caller(config.callerSkip)
	if !ok {
		// This branch cannot easily be tested, so ignore it in coverage reports.
		return thing
	}

	fn := runtime.FuncForPC(pc)
	if fn == nil {
		// This branch cannot easily be tested, so ignore it in coverage reports.
		return thing
	}

	fullName := fn.Name()
	shortName := extractFunctionName(fullName)
	return namer(shortName, thing)

}

// extractFunctionName extracts the simple function name from a full Go function path.
//
// Examples:
//   - "github.com/sam-fredrickson/flow.CreateDatabase" -> "CreateDatabase"
//   - "main.(*Server).HandleRequest" -> "HandleRequest"
//   - "github.com/user/pkg.init.0" -> "init.0"
func extractFunctionName(fullName string) string {
	// Split by path separators to get the last component
	parts := strings.Split(fullName, "/")
	lastPart := parts[len(parts)-1]

	// Handle package.FunctionName or package.(*Type).Method
	// After taking everything after the last ".", we get just the function/method name
	if idx := strings.LastIndex(lastPart, "."); idx != -1 {
		lastPart = lastPart[idx+1:]
	}

	return lastPart
}

// Package awsfake provides an in-process fake AWS backend using Smithy
// middleware interception. No Docker, no AWS account, no LocalStack.
package awsfake

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// handler is the normalized signature for all fake API operations.
type handler func(context.Context, any) (any, error)

// Backend holds the fake AWS state, error injection, and configuration.
type Backend struct {
	RDS *RDSFake
	EC2 *EC2Fake
	IAM *IAMFake

	region    string
	accountID string

	handlers map[string]handler // operation name → handler, built via reflection

	mu          sync.Mutex
	faultInject map[string]*faultInjection
}

type faultInjection struct {
	err         error
	probability float64 // 0.0–1.0, chance of failure per call
}

// New creates a new fake AWS backend for the given region.
func New(region string) *Backend {
	b := &Backend{
		region:      region,
		accountID:   "123456789012",
		faultInject: make(map[string]*faultInjection),
	}
	b.EC2 = NewEC2Fake()
	b.RDS = NewRDSFake(b)
	b.IAM = NewIAMFake(b)
	b.handlers = buildHandlers(b.EC2, b.RDS, b.IAM)
	return b
}

// buildHandlers discovers all exported methods on the given fakes that match
// the handler signature func(context.Context, any) (SomeOutput, error) and
// registers them by method name. Adding a new fake operation is just "add
// the method" — no dispatch table to maintain.
func buildHandlers(fakes ...any) map[string]handler {
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errType := reflect.TypeOf((*error)(nil)).Elem()

	handlers := make(map[string]handler)
	for _, fake := range fakes {
		v := reflect.ValueOf(fake)
		t := v.Type()
		for i := range t.NumMethod() {
			mt := t.Method(i)
			ft := mt.Type
			// Match: receiver + context.Context + any → (output, error)
			if ft.NumIn() != 3 || ft.NumOut() != 2 {
				continue
			}
			if !ft.In(1).Implements(ctxType) {
				continue
			}
			if !ft.Out(1).Implements(errType) {
				continue
			}
			m := v.Method(i)
			handlers[mt.Name] = func(ctx context.Context, params any) (any, error) {
				results := m.Call([]reflect.Value{
					reflect.ValueOf(ctx),
					reflect.ValueOf(params),
				})
				result := results[0].Interface()
				if !results[1].IsNil() {
					return result, results[1].Interface().(error)
				}
				return result, nil
			}
		}
	}
	return handlers
}

// InjectFault registers a stochastic fault for the given operation name.
// Each call to that operation has the given probability (0.0–1.0) of returning the error.
func (b *Backend) InjectFault(operation string, err error, probability float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.faultInject[operation] = &faultInjection{err: err, probability: probability}
}

// checkInjectedError returns an injected error if a stochastic fault fires
// for the operation. Returns nil if no injection is active or the dice roll passes.
func (b *Backend) checkInjectedError(operation string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	inj, ok := b.faultInject[operation]
	if !ok {
		return nil
	}
	// #nosec G404 - this is test code
	if rand.Float64() < inj.probability {
		return inj.err
	}
	return nil
}

// ARN constructs an ARN string for the given service, resource type, and name.
func (b *Backend) ARN(service, resourceType, name string) string {
	return fmt.Sprintf("arn:aws:%s:%s:%s:%s/%s",
		service, b.region, b.accountID, resourceType, name)
}

// paramsKey is a context key for storing the original API input parameters.
// The Initialize middleware saves them so the Deserialize middleware can access them.
type paramsKey struct{}

// Config returns an aws.Config wired to use this fake backend.
// All API calls will be intercepted and dispatched to the in-process fakes.
func (b *Backend) Config() aws.Config {
	cfg := aws.Config{
		Region: b.region,
	}
	cfg.APIOptions = append(cfg.APIOptions, func(stack *smithymiddleware.Stack) error {
		// 1. Initialize middleware: save the original input params in context.
		if err := stack.Initialize.Add(&paramCapture{}, smithymiddleware.Before); err != nil {
			return err
		}

		// 2. Remove Build, Finalize, and Deserialize steps so no real
		//    HTTP requests are made.
		stack.Build.Clear()
		stack.Finalize.Clear()
		stack.Deserialize.Clear()

		// 3. Add our fake deserializer that dispatches to the fakes.
		return stack.Deserialize.Add(&fakeDeserializer{backend: b}, smithymiddleware.Before)
	})
	return cfg
}

// paramCapture saves the original API input parameters into the context
// during the Initialize phase.
type paramCapture struct{}

func (p *paramCapture) ID() string { return "FakeParamCapture" }

func (p *paramCapture) HandleInitialize(
	ctx context.Context,
	in smithymiddleware.InitializeInput,
	next smithymiddleware.InitializeHandler,
) (smithymiddleware.InitializeOutput, smithymiddleware.Metadata, error) {
	ctx = context.WithValue(ctx, paramsKey{}, in.Parameters)
	return next.HandleInitialize(ctx, in)
}

// fakeDeserializer intercepts all API calls and dispatches them to the fakes.
type fakeDeserializer struct {
	backend *Backend
}

func (f *fakeDeserializer) ID() string { return "FakeDeserializer" }

func (f *fakeDeserializer) HandleDeserialize(
	ctx context.Context,
	in smithymiddleware.DeserializeInput,
	next smithymiddleware.DeserializeHandler,
) (smithymiddleware.DeserializeOutput, smithymiddleware.Metadata, error) {
	operationName := smithymiddleware.GetOperationName(ctx)

	// Check for injected errors first.
	if err := f.backend.checkInjectedError(operationName); err != nil {
		return smithymiddleware.DeserializeOutput{}, smithymiddleware.Metadata{}, err
	}

	// Retrieve the original input parameters saved by paramCapture.
	params := ctx.Value(paramsKey{})

	// Dispatch to the appropriate fake.
	result, err := f.backend.dispatch(ctx, operationName, params)
	if err != nil {
		return smithymiddleware.DeserializeOutput{}, smithymiddleware.Metadata{}, err
	}

	return smithymiddleware.DeserializeOutput{
		RawResponse: &smithyhttp.Response{
			Response: &dummyHTTPResponse,
		},
		Result: result,
	}, smithymiddleware.Metadata{}, nil
}

// dispatch routes an API call to the correct fake based on operation name.
func (b *Backend) dispatch(ctx context.Context, operation string, params any) (any, error) {
	// Baseline latency for all API calls, plus extra for mutating operations.
	// #nosec G404 - this is test code
	latency := 1 + rand.IntN(3) // 1–3ms baseline
	if !strings.HasPrefix(operation, "Describe") &&
		!strings.HasPrefix(operation, "Get") &&
		!strings.HasPrefix(operation, "List") {
		latency += 10 + rand.IntN(20) // #nosec G404 - +10–29ms for mutations
	}
	time.Sleep(time.Duration(latency) * time.Millisecond)

	h, ok := b.handlers[operation]
	if !ok {
		return nil, fmt.Errorf("awsfake: unsupported operation %q", operation)
	}
	return h(ctx, params)
}

// RandomID generates a random ID with the given prefix and hex length.
// For example, RandomID("vpc", 8) might return "vpc-a1b2c3d4".
func RandomID(prefix string, length int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, length)
	for i := range b {
		// #nosec G404 - this is test code
		b[i] = hex[rand.IntN(len(hex))]
	}
	return prefix + "-" + string(b)
}

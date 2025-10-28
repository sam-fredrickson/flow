// SPDX-License-Identifier: Apache-2.0

package flow_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sam-fredrickson/flow"
)

// BenchConfig controls the behavior of benchmark operations.
type BenchConfig struct {
	// StepDuration is the simulated I/O delay per step.
	// Set to 0 for CPU-bound benchmarks, >0 to simulate I/O operations.
	StepDuration time.Duration
}

// DeploymentState represents the state for a deployment workflow.
type DeploymentState struct {
	mu sync.Mutex // Protects fields below for parallel execution

	Config struct {
		Port         int
		HasDatabase  bool
		Environment  string
		RetryCount   int
		ServiceNames []string
	}

	// Execution tracking
	StepsExecuted    int32 // atomic counter
	ValidationsDone  int
	ServicesStarted  []string
	DNSUpdated       bool
	NotificationSent bool

	// Simulated I/O delay
	StepDuration time.Duration
}

func newDeploymentState(cfg BenchConfig) *DeploymentState {
	s := &DeploymentState{
		StepDuration: cfg.StepDuration,
	}
	s.Config.Port = 8080
	s.Config.HasDatabase = true
	s.Config.Environment = "production"
	s.Config.ServiceNames = []string{"api", "worker", "cache"}
	return s
}

// simulateWork performs minimal work to prevent compiler elimination
// and optionally simulates I/O delay.
func (s *DeploymentState) simulateWork() {
	atomic.AddInt32(&s.StepsExecuted, 1)
	if s.StepDuration > 0 {
		time.Sleep(s.StepDuration)
	}
}

// =============================================================================
// Flow Library Implementation
// =============================================================================

// ValidateConfig checks basic configuration validity.
func ValidateConfig() flow.Step[*DeploymentState] {
	return func(ctx context.Context, s *DeploymentState) error {
		s.simulateWork()
		s.mu.Lock()
		s.ValidationsDone++
		s.mu.Unlock()
		if s.Config.Port == 0 {
			return errors.New("port required")
		}
		return nil
	}
}

// HasDatabase checks if database is configured.
func HasDatabase() flow.Predicate[*DeploymentState] {
	return func(ctx context.Context, s *DeploymentState) (bool, error) {
		s.simulateWork()
		return s.Config.HasDatabase, nil
	}
}

// SetupDatabase initializes the database.
func SetupDatabase() flow.Step[*DeploymentState] {
	return func(ctx context.Context, s *DeploymentState) error {
		s.simulateWork()
		return nil
	}
}

// DeployService deploys a named service.
func DeployService(name string) flow.Step[*DeploymentState] {
	return func(ctx context.Context, s *DeploymentState) error {
		s.simulateWork()
		s.mu.Lock()
		s.ServicesStarted = append(s.ServicesStarted, name)
		s.mu.Unlock()
		return nil
	}
}

// RunHealthCheck performs a health check.
func RunHealthCheck() flow.Step[*DeploymentState] {
	return func(ctx context.Context, s *DeploymentState) error {
		s.simulateWork()
		return nil
	}
}

// UpdateDNS updates DNS records.
func UpdateDNS() flow.Step[*DeploymentState] {
	return func(ctx context.Context, s *DeploymentState) error {
		s.simulateWork()
		s.mu.Lock()
		s.DNSUpdated = true
		s.mu.Unlock()
		// Simulate occasional transient failure
		if s.Config.RetryCount == 0 {
			s.mu.Lock()
			s.Config.RetryCount++
			s.mu.Unlock()
			return errors.New("transient DNS error")
		}
		return nil
	}
}

// SendNotification sends a deployment notification.
func SendNotification() flow.Step[*DeploymentState] {
	return func(ctx context.Context, s *DeploymentState) error {
		s.simulateWork()
		s.mu.Lock()
		s.NotificationSent = true
		s.mu.Unlock()
		return nil
	}
}

// GetServiceNames extracts service names from state.
func GetServiceNames() flow.Extract[*DeploymentState, []string] {
	return func(ctx context.Context, s *DeploymentState) ([]string, error) {
		return s.Config.ServiceNames, nil
	}
}

// DeployWorkflow_Flow implements the deployment workflow using the flow library.
func DeployWorkflow_Flow() flow.Step[*DeploymentState] {
	return flow.Do(
		ValidateConfig(),
		flow.When(HasDatabase(), SetupDatabase()),
		flow.InParallel(
			flow.From(
				GetServiceNames(),
				flow.Map(DeployService),
			),
		),
		RunHealthCheck(),
		flow.Retry(
			UpdateDNS(),
			flow.UpTo(3),
			flow.ExponentialBackoff(1*time.Microsecond),
		),
		flow.IgnoreError(SendNotification()),
	)
}

// DeployWorkflow_FlowStatic is a package-level variable initialized once.
// This gives the Go compiler a chance to optimize more aggressively since
// the workflow structure is known at compile time.
var DeployWorkflow_FlowStatic = flow.Do(
	ValidateConfig(),
	flow.When(HasDatabase(), SetupDatabase()),
	flow.InParallel(
		flow.From(
			GetServiceNames(),
			flow.Map(DeployService),
		),
	),
	RunHealthCheck(),
	flow.Retry(
		UpdateDNS(),
		flow.UpTo(3),
		flow.ExponentialBackoff(1*time.Microsecond),
	),
	flow.IgnoreError(SendNotification()),
)

// =============================================================================
// Traditional Go Implementation
// =============================================================================

// Traditional step functions - lowercase names following Go conventions
// for package-internal functions.

func validateConfig(ctx context.Context, s *DeploymentState) error {
	s.simulateWork()
	s.mu.Lock()
	s.ValidationsDone++
	s.mu.Unlock()
	if s.Config.Port == 0 {
		return errors.New("port required")
	}
	return nil
}

func hasDatabase(ctx context.Context, s *DeploymentState) bool {
	s.simulateWork()
	return s.Config.HasDatabase
}

func setupDatabase(ctx context.Context, s *DeploymentState) error {
	s.simulateWork()
	return nil
}

func deployService(ctx context.Context, s *DeploymentState, name string) error {
	s.simulateWork()
	s.mu.Lock()
	s.ServicesStarted = append(s.ServicesStarted, name)
	s.mu.Unlock()
	return nil
}

func deployServicesParallel(ctx context.Context, s *DeploymentState) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(s.Config.ServiceNames))
	for _, name := range s.Config.ServiceNames {
		wg.Add(1)
		go func(svcName string) {
			defer wg.Done()
			if err := deployService(ctx, s, svcName); err != nil {
				errCh <- err
			}
		}(name)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func runHealthCheck(ctx context.Context, s *DeploymentState) error {
	s.simulateWork()
	return nil
}

func updateDNS(ctx context.Context, s *DeploymentState) error {
	s.simulateWork()
	s.mu.Lock()
	s.DNSUpdated = true
	if s.Config.RetryCount == 0 {
		s.Config.RetryCount++
		s.mu.Unlock()
		return errors.New("transient DNS error")
	}
	s.mu.Unlock()
	return nil
}

func updateDNSWithRetry(ctx context.Context, s *DeploymentState) error {
	var lastErr error
	backoff := 1 * time.Microsecond
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}
		if err := updateDNS(ctx, s); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func sendNotification(ctx context.Context, s *DeploymentState) error {
	s.simulateWork()
	s.mu.Lock()
	s.NotificationSent = true
	s.mu.Unlock()
	return nil
}

func deployWorkflow_Traditional(ctx context.Context, s *DeploymentState) error {
	if err := validateConfig(ctx, s); err != nil {
		return err
	}

	if hasDatabase(ctx, s) {
		if err := setupDatabase(ctx, s); err != nil {
			return err
		}
	}

	if err := deployServicesParallel(ctx, s); err != nil {
		return err
	}

	if err := runHealthCheck(ctx, s); err != nil {
		return err
	}

	if err := updateDNSWithRetry(ctx, s); err != nil {
		return err
	}

	// Best effort - ignore errors
	_ = sendNotification(ctx, s)

	return nil
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkFlow_Simple(b *testing.B) {
	configs := []struct {
		name     string
		duration time.Duration
	}{
		{"cpu_bound", 0},
		{"io_1us", 1 * time.Microsecond},
		{"io_10us", 10 * time.Microsecond},
		{"io_100us", 100 * time.Microsecond},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			benchCfg := BenchConfig{StepDuration: cfg.duration}
			simpleWorkflow := flow.Do(
				ValidateConfig(),
				RunHealthCheck(),
			)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := newDeploymentState(benchCfg)
				if err := simpleWorkflow(context.Background(), state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTraditional_Simple(b *testing.B) {
	configs := []struct {
		name     string
		duration time.Duration
	}{
		{"cpu_bound", 0},
		{"io_1us", 1 * time.Microsecond},
		{"io_10us", 10 * time.Microsecond},
		{"io_100us", 100 * time.Microsecond},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			benchCfg := BenchConfig{StepDuration: cfg.duration}
			simpleWorkflow := func(ctx context.Context, s *DeploymentState) error {
				if err := validateConfig(ctx, s); err != nil {
					return err
				}
				return runHealthCheck(ctx, s)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := newDeploymentState(benchCfg)
				if err := simpleWorkflow(context.Background(), state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFlow_Parallel(b *testing.B) {
	configs := []struct {
		name     string
		duration time.Duration
	}{
		{"cpu_bound", 0},
		{"io_1us", 1 * time.Microsecond},
		{"io_10us", 10 * time.Microsecond},
		{"io_100us", 100 * time.Microsecond},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			benchCfg := BenchConfig{StepDuration: cfg.duration}
			parallelWorkflow := flow.InParallel(
				flow.From(
					GetServiceNames(),
					flow.Map(DeployService),
				),
			)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := newDeploymentState(benchCfg)
				if err := parallelWorkflow(context.Background(), state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTraditional_Parallel(b *testing.B) {
	configs := []struct {
		name     string
		duration time.Duration
	}{
		{"cpu_bound", 0},
		{"io_1us", 1 * time.Microsecond},
		{"io_10us", 10 * time.Microsecond},
		{"io_100us", 100 * time.Microsecond},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			benchCfg := BenchConfig{StepDuration: cfg.duration}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := newDeploymentState(benchCfg)
				if err := deployServicesParallel(context.Background(), state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFlow_Full(b *testing.B) {
	configs := []struct {
		name     string
		duration time.Duration
	}{
		{"cpu_bound", 0},
		{"io_1us", 1 * time.Microsecond},
		{"io_10us", 10 * time.Microsecond},
		{"io_100us", 100 * time.Microsecond},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			benchCfg := BenchConfig{StepDuration: cfg.duration}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := newDeploymentState(benchCfg)
				if err := DeployWorkflow_Flow()(context.Background(), state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFlow_FullStatic(b *testing.B) {
	configs := []struct {
		name     string
		duration time.Duration
	}{
		{"cpu_bound", 0},
		{"io_1us", 1 * time.Microsecond},
		{"io_10us", 10 * time.Microsecond},
		{"io_100us", 100 * time.Microsecond},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			benchCfg := BenchConfig{StepDuration: cfg.duration}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := newDeploymentState(benchCfg)
				if err := DeployWorkflow_FlowStatic(context.Background(), state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTraditional_Full(b *testing.B) {
	configs := []struct {
		name     string
		duration time.Duration
	}{
		{"cpu_bound", 0},
		{"io_1us", 1 * time.Microsecond},
		{"io_10us", 10 * time.Microsecond},
		{"io_100us", 100 * time.Microsecond},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			benchCfg := BenchConfig{StepDuration: cfg.duration}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := newDeploymentState(benchCfg)
				if err := deployWorkflow_Traditional(context.Background(), state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

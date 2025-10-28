// SPDX-License-Identifier: Apache-2.0

package flow_test

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// TestBehaviorMatch_Simple verifies that both implementations behave identically
// for simple sequential workflows.
func TestBehaviorMatch_Simple(t *testing.T) {
	tests := []struct {
		name        string
		initialPort int
		wantErr     bool
	}{
		{
			name:        "valid_config",
			initialPort: 8080,
			wantErr:     false,
		},
		{
			name:        "invalid_config",
			initialPort: 0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Run flow version
			flowState := newDeploymentState(BenchConfig{})
			flowState.Config.Port = tt.initialPort
			flowState.Config.ServiceNames = nil // Simple workflow doesn't use services

			simpleFlow := ValidateConfig()
			flowErr := simpleFlow(ctx, flowState)

			// Run traditional version
			tradState := newDeploymentState(BenchConfig{})
			tradState.Config.Port = tt.initialPort
			tradState.Config.ServiceNames = nil

			tradErr := validateConfig(ctx, tradState)

			// Compare errors
			if (flowErr != nil) != (tradErr != nil) {
				t.Errorf("error mismatch: flow=%v, traditional=%v", flowErr, tradErr)
			}
			if (flowErr != nil) != tt.wantErr {
				t.Errorf("expected error=%v, got=%v", tt.wantErr, flowErr)
			}

			// Compare state
			if flowErr == nil {
				if flowState.ValidationsDone != tradState.ValidationsDone {
					t.Errorf("ValidationsDone mismatch: flow=%d, traditional=%d",
						flowState.ValidationsDone, tradState.ValidationsDone)
				}
			}
		})
	}
}

// TestBehaviorMatch_Full verifies that both full implementations produce
// identical results.
func TestBehaviorMatch_Full(t *testing.T) {
	tests := []struct {
		name        string
		hasDatabase bool
	}{
		{
			name:        "with_database",
			hasDatabase: true,
		},
		{
			name:        "without_database",
			hasDatabase: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Run flow version
			flowState := newDeploymentState(BenchConfig{})
			flowState.Config.HasDatabase = tt.hasDatabase
			flowErr := DeployWorkflow_Flow()(ctx, flowState)

			// Run traditional version
			tradState := newDeploymentState(BenchConfig{})
			tradState.Config.HasDatabase = tt.hasDatabase
			tradErr := deployWorkflow_Traditional(ctx, tradState)

			// Compare errors
			if (flowErr != nil) != (tradErr != nil) {
				t.Errorf("error mismatch: flow=%v, traditional=%v", flowErr, tradErr)
			}

			// Compare final state if no errors
			if flowErr == nil && tradErr == nil {
				compareDeploymentState(t, flowState, tradState)
			}
		})
	}
}

// TestWorkflowSteps_Flow verifies the flow implementation executes all expected steps.
func TestWorkflowSteps_Flow(t *testing.T) {
	ctx := context.Background()
	state := newDeploymentState(BenchConfig{})

	err := DeployWorkflow_Flow()(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify expected state
	if state.ValidationsDone != 1 {
		t.Errorf("expected 1 validation, got %d", state.ValidationsDone)
	}

	if !state.DNSUpdated {
		t.Error("expected DNS to be updated")
	}

	if !state.NotificationSent {
		t.Error("expected notification to be sent")
	}

	// Verify all services were deployed
	expectedServices := []string{"api", "worker", "cache"}
	if len(state.ServicesStarted) != len(expectedServices) {
		t.Errorf("expected %d services, got %d", len(expectedServices), len(state.ServicesStarted))
	}

	// Sort for comparison (parallel execution order is non-deterministic)
	sort.Strings(state.ServicesStarted)
	sort.Strings(expectedServices)
	if !reflect.DeepEqual(state.ServicesStarted, expectedServices) {
		t.Errorf("services mismatch: got %v, want %v", state.ServicesStarted, expectedServices)
	}

	// Verify minimum number of steps executed (accounting for retry)
	minExpectedSteps := int32(8) // validate, hasDB, setupDB, 3 services, health, DNS attempts, notify
	if state.StepsExecuted < minExpectedSteps {
		t.Errorf("expected at least %d steps, got %d", minExpectedSteps, state.StepsExecuted)
	}
}

// TestWorkflowSteps_Traditional verifies the traditional implementation executes
// all expected steps.
func TestWorkflowSteps_Traditional(t *testing.T) {
	ctx := context.Background()
	state := newDeploymentState(BenchConfig{})

	err := deployWorkflow_Traditional(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify expected state
	if state.ValidationsDone != 1 {
		t.Errorf("expected 1 validation, got %d", state.ValidationsDone)
	}

	if !state.DNSUpdated {
		t.Error("expected DNS to be updated")
	}

	if !state.NotificationSent {
		t.Error("expected notification to be sent")
	}

	// Verify all services were deployed
	expectedServices := []string{"api", "worker", "cache"}
	if len(state.ServicesStarted) != len(expectedServices) {
		t.Errorf("expected %d services, got %d", len(expectedServices), len(state.ServicesStarted))
	}

	// Sort for comparison (parallel execution order is non-deterministic)
	sort.Strings(state.ServicesStarted)
	sort.Strings(expectedServices)
	if !reflect.DeepEqual(state.ServicesStarted, expectedServices) {
		t.Errorf("services mismatch: got %v, want %v", state.ServicesStarted, expectedServices)
	}

	// Verify minimum number of steps executed (accounting for retry)
	minExpectedSteps := int32(8) // validate, hasDB check, setupDB, 3 services, health, DNS attempts, notify
	if state.StepsExecuted < minExpectedSteps {
		t.Errorf("expected at least %d steps, got %d", minExpectedSteps, state.StepsExecuted)
	}
}

// TestRetryBehavior verifies that retry logic works correctly in both implementations.
func TestRetryBehavior(t *testing.T) {
	ctx := context.Background()

	// Flow version
	flowState := newDeploymentState(BenchConfig{})
	flowErr := DeployWorkflow_Flow()(ctx, flowState)
	if flowErr != nil {
		t.Errorf("flow workflow failed: %v", flowErr)
	}
	flowRetries := flowState.Config.RetryCount

	// Traditional version
	tradState := newDeploymentState(BenchConfig{})
	tradErr := deployWorkflow_Traditional(ctx, tradState)
	if tradErr != nil {
		t.Errorf("traditional workflow failed: %v", tradErr)
	}
	tradRetries := tradState.Config.RetryCount

	// Both should have retried at least once
	if flowRetries < 1 {
		t.Errorf("flow implementation should have retried, got %d retries", flowRetries)
	}
	if tradRetries < 1 {
		t.Errorf("traditional implementation should have retried, got %d retries", tradRetries)
	}

	// Both should succeed on retry
	if flowState.DNSUpdated != tradState.DNSUpdated {
		t.Error("DNSUpdated mismatch between implementations")
	}
}

// TestConditionalExecution verifies that conditional logic works identically.
func TestConditionalExecution(t *testing.T) {
	tests := []struct {
		name        string
		hasDatabase bool
	}{
		{"database_enabled", true},
		{"database_disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Flow version
			flowState := newDeploymentState(BenchConfig{})
			flowState.Config.HasDatabase = tt.hasDatabase
			flowErr := DeployWorkflow_Flow()(ctx, flowState)

			// Traditional version
			tradState := newDeploymentState(BenchConfig{})
			tradState.Config.HasDatabase = tt.hasDatabase
			tradErr := deployWorkflow_Traditional(ctx, tradState)

			// Both should succeed
			if flowErr != nil {
				t.Errorf("flow workflow failed: %v", flowErr)
			}
			if tradErr != nil {
				t.Errorf("traditional workflow failed: %v", tradErr)
			}

			// Verify both executed the same number of steps
			// The difference in step count should be minimal (just the predicate check)
			stepDiff := flowState.StepsExecuted - tradState.StepsExecuted
			if stepDiff < -1 || stepDiff > 1 {
				t.Errorf("step count differs significantly: flow=%d, traditional=%d",
					flowState.StepsExecuted, tradState.StepsExecuted)
			}
		})
	}
}

// compareDeploymentState compares two DeploymentState instances for equality.
func compareDeploymentState(t *testing.T, flow, trad *DeploymentState) {
	t.Helper()

	if flow.ValidationsDone != trad.ValidationsDone {
		t.Errorf("ValidationsDone mismatch: flow=%d, traditional=%d",
			flow.ValidationsDone, trad.ValidationsDone)
	}

	if flow.DNSUpdated != trad.DNSUpdated {
		t.Errorf("DNSUpdated mismatch: flow=%v, traditional=%v",
			flow.DNSUpdated, trad.DNSUpdated)
	}

	if flow.NotificationSent != trad.NotificationSent {
		t.Errorf("NotificationSent mismatch: flow=%v, traditional=%v",
			flow.NotificationSent, trad.NotificationSent)
	}

	// Sort service lists for comparison (parallel execution order varies)
	flowServices := make([]string, len(flow.ServicesStarted))
	tradServices := make([]string, len(trad.ServicesStarted))
	copy(flowServices, flow.ServicesStarted)
	copy(tradServices, trad.ServicesStarted)
	sort.Strings(flowServices)
	sort.Strings(tradServices)

	if !reflect.DeepEqual(flowServices, tradServices) {
		t.Errorf("ServicesStarted mismatch: flow=%v, traditional=%v",
			flowServices, tradServices)
	}
}

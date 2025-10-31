// SPDX-License-Identifier: Apache-2.0

// Package main demonstrates basic tracing functionality in the flow library.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sam-fredrickson/flow"
)

type DeploymentState struct {
	AppName    string
	Version    string
	ConfigPath string
}

// ValidateConfig validates the deployment configuration.
func ValidateConfig(ctx context.Context, state *DeploymentState) error {
	time.Sleep(10 * time.Millisecond)
	if state.AppName == "" {
		return fmt.Errorf("app name is required")
	}
	if state.Version == "" {
		return fmt.Errorf("version is required")
	}
	return nil
}

// BuildArtifact builds the deployment artifact.
func BuildArtifact(ctx context.Context, state *DeploymentState) error {
	time.Sleep(150 * time.Millisecond)
	fmt.Printf("Building %s version %s...\n", state.AppName, state.Version)
	return nil
}

// RunTests runs the test suite.
func RunTests(ctx context.Context, state *DeploymentState) error {
	time.Sleep(300 * time.Millisecond)
	fmt.Println("Running tests...")
	return nil
}

// DeployToStaging deploys to staging environment.
func DeployToStaging(ctx context.Context, state *DeploymentState) error {
	time.Sleep(200 * time.Millisecond)
	fmt.Println("Deploying to staging...")
	return nil
}

// VerifyDeployment verifies the deployment.
func VerifyDeployment(ctx context.Context, state *DeploymentState) error {
	time.Sleep(100 * time.Millisecond)
	fmt.Println("Verifying deployment...")
	return nil
}

func main() {
	// Create a deployment workflow with named steps
	workflow := flow.Do(
		flow.Named("validate", ValidateConfig),
		flow.Named("build", BuildArtifact),
		flow.Named("test", RunTests),
		flow.Named("deploy", DeployToStaging),
		flow.Named("verify", VerifyDeployment),
	)

	state := &DeploymentState{
		AppName: "my-app",
		Version: "v1.2.3",
	}

	ctx := context.Background()

	// Example 1: Trace and write text output
	fmt.Println("=== Example 1: Basic Text Trace ===")
	err := flow.Spawn(
		flow.Traced(workflow),
		flow.WriteTextTo(os.Stdout),
	)(ctx, state)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Example 2: Trace and write JSON output
	fmt.Println("\n=== Example 2: JSON Trace ===")
	err = flow.Spawn(
		flow.Traced(workflow),
		flow.WriteJSONTo(os.Stdout),
	)(ctx, state)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Example 3: Capture trace for analysis
	fmt.Println("\n=== Example 3: Trace Analysis ===")
	trace, err := flow.Traced(workflow)(ctx, state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Total steps: %d\n", trace.TotalSteps)
	fmt.Printf("Errors: %d\n", trace.TotalErrors)
	fmt.Printf("Total duration: %v\n", trace.Duration)

	// Example 4: Filter slow steps (> 100ms)
	fmt.Println("\n=== Example 4: Filter Slow Steps (>100ms) ===")
	slowSteps := trace.Filter(flow.MinDuration(100 * time.Millisecond))
	if _, err := slowSteps.WriteText(os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing trace: %v\n", err)
	}

	// Example 5: Filter by name pattern
	fmt.Println("\n=== Example 5: Filter Steps Matching 'test' ===")
	testSteps := trace.Filter(flow.NameMatches("test*"))
	if _, err := testSteps.WriteText(os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing trace: %v\n", err)
	}

	// Example 6: Combined filters - fast successful steps
	fmt.Println("\n=== Example 6: Fast Successful Steps (<50ms) ===")
	fastAndSuccessful := trace.Filter(
		flow.MaxDuration(50*time.Millisecond),
		flow.NoError(),
	)
	fmt.Printf("Found %d fast successful steps\n", fastAndSuccessful.TotalSteps)
}

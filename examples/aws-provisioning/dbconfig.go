package main

import (
	"context"

	"github.com/sam-fredrickson/flow"
)

// DBSetupContext is the child state for per-instance database setup.
// It represents being connected to a specific DB instance.
type DBSetupContext struct {
	InstanceID  string
	InstanceARN string
}

// InstanceSetup groups all the service setup steps for one DB instance.
type InstanceSetup struct {
	InstanceID string
	Steps      []flow.Step[*DBSetupContext]
}

// ConfigureDatabases runs the config-driven DB setup pattern:
// gather service declarations by instance, then run per-instance setup in parallel.
func ConfigureDatabases() flow.Step[*Env] {
	return flow.AutoNamed(flow.InParallel(flow.ForEach(
		gatherDBSetup,
		runInstanceSetup,
	)))
}

// gatherDBSetup groups service DB configurations by instance (the "gather" phase).
func gatherDBSetup(_ context.Context, env *Env) ([]InstanceSetup, error) {
	byInstance := map[string][]flow.Step[*DBSetupContext]{}

	for serviceName, cfg := range env.Config.Services {
		for _, db := range cfg.Databases {
			if db.Setup == nil {
				continue
			}
			// Wrap with service name for error attribution.
			wrappedStep := flow.Named(serviceName, db.Setup)
			byInstance[db.Instance] = append(byInstance[db.Instance], wrappedStep)
		}
	}

	var setups []InstanceSetup
	for id, steps := range byInstance {
		setups = append(setups, InstanceSetup{
			InstanceID: id,
			Steps:      steps,
		})
	}
	return setups, nil
}

// runInstanceSetup runs all service setup steps for one instance using Spawn.
func runInstanceSetup(setup InstanceSetup) flow.Step[*Env] {
	return flow.Named(setup.InstanceID,
		flow.Spawn(
			deriveDBContext(setup.InstanceID),
			flow.InParallel(flow.Steps(setup.Steps...)),
		),
	)
}

// deriveDBContext creates the child state for per-instance DB setup.
func deriveDBContext(instanceID string) flow.Extract[*Env, *DBSetupContext] {
	return func(_ context.Context, env *Env) (*DBSetupContext, error) {
		env.mu.Lock()
		arn := env.DBInstances[instanceID]
		env.mu.Unlock()
		return &DBSetupContext{
			InstanceID:  instanceID,
			InstanceARN: arn,
		}, nil
	}
}

// SQL-level step constructors — stubs since RDS doesn't have SQL APIs.
// The point is exercising flow composition (Do, InParallel, ForEach, Spawn, etc.)
// These use AutoNamed(Named(...)) so traces show both the constructor name and its parameters.

func CreateDatabase(name string) flow.Step[*DBSetupContext] {
	return flow.AutoNamed(flow.Named(name, func(_ context.Context, _ *DBSetupContext) error {
		// Would execute: CREATE DATABASE IF NOT EXISTS <name>
		return nil
	}))
}

func CreateUser(username string) flow.Step[*DBSetupContext] {
	return flow.AutoNamed(flow.Named(username, func(_ context.Context, _ *DBSetupContext) error {
		// Would execute: CREATE USER '<username>'@'%' IDENTIFIED BY '...'
		return nil
	}))
}

func GrantReadWrite(username, database string) flow.Step[*DBSetupContext] {
	return flow.AutoNamed(flow.Named(username+"@"+database, func(_ context.Context, _ *DBSetupContext) error {
		// Would execute: GRANT SELECT, INSERT, UPDATE, DELETE ON <database>.* TO '<username>'@'%'
		return nil
	}))
}

func GrantReadOnly(username, database string) flow.Step[*DBSetupContext] {
	return flow.AutoNamed(flow.Named(username+"@"+database, func(_ context.Context, _ *DBSetupContext) error {
		// Would execute: GRANT SELECT ON <database>.* TO '<username>'@'%'
		return nil
	}))
}

func CreateMonitoringUser(database string) flow.Step[*DBSetupContext] {
	return flow.AutoNamed(flow.Named(database, func(_ context.Context, _ *DBSetupContext) error {
		// Would execute: CREATE USER 'monitoring'@'%'; GRANT pg_monitor TO monitoring;
		return nil
	}))
}

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"time"

	"github.com/sam-fredrickson/flow"
)

type EnvironmentConfig struct {
	Name   string
	Region string
}

type EnvironmentSetup struct {
	Services *ServicesConfig
}

func NewEnvironmentSetup(config EnvironmentConfig, services *ServicesConfig) *EnvironmentSetup {
	return &EnvironmentSetup{
		Services: services,
	}
}

func (setup *EnvironmentSetup) Run(ctx context.Context) error {
	return SetupEnvironment()(ctx, setup)
}

func SetupEnvironment() flow.Step[*EnvironmentSetup] {
	return flow.Do(
		Phase1(),
		Phase2(),
	)
}

func Phase1() flow.Step[*EnvironmentSetup] {
	return flow.Do(
		CreateAwsAccount(),
		CreateTerraformWorkspace(),
		GenerateTerraformFiles(),
		RunTerraformPlan(),
		ApplyTerraformPlan(),
	)
}

func CreateAwsAccount() flow.Step[*EnvironmentSetup] {
	return flow.AutoNamed(flow.Retry(
		func(ctx context.Context, setup *EnvironmentSetup) error {
			// Would create AWS account via Organizations API
			return nil
		},
		flow.OnlyIf(isTransientAWSError),
		flow.UpTo(10),
		flow.ExponentialBackoff(2*time.Second),
	))
}

func CreateTerraformWorkspace() flow.Step[*EnvironmentSetup] {
	return flow.AutoNamed(flow.Retry(
		func(ctx context.Context, setup *EnvironmentSetup) error {
			// Would create Terraform Cloud workspace
			return nil
		},
		flow.UpTo(5),
		flow.ExponentialBackoff(1*time.Second),
	))
}

func GenerateTerraformFiles() flow.Step[*EnvironmentSetup] {
	return flow.AutoNamed(func(ctx context.Context, setup *EnvironmentSetup) error {
		// Would generate .tf files from templates
		return nil
	})
}

func RunTerraformPlan() flow.Step[*EnvironmentSetup] {
	return flow.AutoNamed(func(ctx context.Context, setup *EnvironmentSetup) error {
		// Would run terraform plan
		return nil
	})
}

func ApplyTerraformPlan() flow.Step[*EnvironmentSetup] {
	return flow.AutoNamed(flow.Retry(
		func(ctx context.Context, setup *EnvironmentSetup) error {
			// Would run terraform apply
			return nil
		},
		flow.UpTo(5),
		flow.ExponentialBackoff(5*time.Second),
	))
}

func Phase2() flow.Step[*EnvironmentSetup] {
	return flow.Do(
		ConfigureDatabases(),
	)
}

func ConfigureDatabases() flow.Step[*EnvironmentSetup] {
	return flow.InParallel(flow.ForEach(
		GatherDbSetup,
		RunDbSetup,
	))
}

func GatherDbSetup(ctx context.Context, setup *EnvironmentSetup) ([]DbInstanceSetup, error) {
	// gather db setup steps from each service config per db instance
	stepsByInstance := map[DatabaseInstance][]flow.Step[*ServiceDbSetup]{}
	for serviceName, cfg := range setup.Services.Configs {
		for _, db := range cfg.Databases {
			if db.Setup == nil {
				continue // skip services with no setup
			}
			// Wrap with service name for error attribution
			wrappedStep := flow.Named(string(serviceName), db.Setup)
			stepsByInstance[db.Instance] = append(stepsByInstance[db.Instance], wrappedStep)
		}
	}
	var allSetups []DbInstanceSetup
	for instance, steps := range stepsByInstance {
		// TODO: actually find details and open DB connection
		allSetups = append(allSetups, DbInstanceSetup{nil, instance, steps})
	}
	return allSetups, nil
}

func RunDbSetup(instance DbInstanceSetup) flow.Step[*EnvironmentSetup] {
	return flow.Spawn(
		PrepareDbSetup(instance),
		flow.InParallel(flow.Steps(instance.Steps...)),
	)
}

func PrepareDbSetup(instance DbInstanceSetup) flow.Extract[*EnvironmentSetup, *ServiceDbSetup] {
	return func(ctx context.Context, env *EnvironmentSetup) (*ServiceDbSetup, error) {
		return &ServiceDbSetup{DB: instance.Connection}, nil
	}
}

type DbInstanceSetup struct {
	Connection       *sql.DB
	DatabaseInstance DatabaseInstance
	Steps            []flow.Step[*ServiceDbSetup]
}

func DefaultServicesConfig() ServicesConfig {
	return ServicesConfig{
		Configs: map[ServiceName]ServiceConfig{
			"webapp": {
				Databases: []ServiceDbConfig{
					{
						Instance: "main",
						Setup: flow.Do(
							CreateDatabase("webapp_db"),
							CreateUser("webapp_user"),
							GrantReadWrite("webapp_user", "webapp_db"),
						),
					},
				},
			},
			"api": {
				Databases: []ServiceDbConfig{
					{
						Instance: "main",
						Setup: flow.Do(
							CreateDatabase("api_db"),
							CreateUser("api_user"),
							GrantReadWrite("api_user", "api_db"),
						),
					},
				},
			},
			"analytics": {
				Databases: []ServiceDbConfig{
					{
						Instance: "data-mart",
						Setup: flow.Do(
							CreateDatabase("analytics_db"),
							CreateUser("analytics_user"),
							GrantReadWrite("analytics_user", "analytics_db"),
							// Special case: data-mart needs binlog retention for replication
							SetBinlogRetention(72),
						),
					},
				},
			},
			"reporting": {
				Databases: []ServiceDbConfig{
					{
						Instance: "data-mart",
						Setup: flow.Do(
							CreateDatabase("reporting_db"),
							CreateUser("reporting_user"),
							GrantReadWrite("reporting_user", "reporting_db"),
							// Also needs read access to analytics data
							GrantReadOnly("reporting_user", "analytics_db"),
						),
					},
				},
			},
			"background-jobs": {
				Databases: []ServiceDbConfig{
					{
						Instance: "main",
						Setup: flow.Do(
							CreateDatabase("jobs_db"),
							CreateUser("jobs_user"),
							GrantReadWrite("jobs_user", "jobs_db"),
						),
					},
					{
						Instance: "cache",
						Setup: flow.Do(
							CreateDatabase("jobs_cache"),
							CreateUser("jobs_cache_user"),
							GrantReadWrite("jobs_cache_user", "jobs_cache"),
						),
					},
				},
			},
		},
	}
}

type ServicesConfig struct {
	Configs map[ServiceName]ServiceConfig
}

type ServiceName = string

type ServiceConfig struct {
	Databases []ServiceDbConfig
}

type DatabaseInstance = string

type ServiceDbConfig struct {
	Instance DatabaseInstance
	Setup    flow.Step[*ServiceDbSetup]
}

type ServiceDbSetup struct {
	DB *sql.DB
}

// DB Setup Step Constructors
// These are stubs for demonstration. Real implementations would execute SQL.

func CreateDatabase(name string) flow.Step[*ServiceDbSetup] {
	return flow.Retry(
		flow.Named("CreateDatabase:"+name, func(ctx context.Context, setup *ServiceDbSetup) error {
			// Would execute: CREATE DATABASE IF NOT EXISTS name
			return nil
		}),
	)
}

func CreateUser(username string) flow.Step[*ServiceDbSetup] {
	return flow.Retry(
		flow.Named("CreateUser:"+username,
			func(ctx context.Context, setup *ServiceDbSetup) error {
				// Would execute: CREATE USER 'username'@'%' IDENTIFIED BY '...'
				return nil
			}),
	)
}

func GrantReadWrite(username, database string) flow.Step[*ServiceDbSetup] {
	return flow.Retry(flow.Named("GrantReadWrite:"+username+"@"+database,
		func(ctx context.Context, setup *ServiceDbSetup) error {
			// Would execute: GRANT SELECT, INSERT, UPDATE, DELETE ON database.* TO 'username'@'%'
			return nil
		}))
}

func GrantReadOnly(username, database string) flow.Step[*ServiceDbSetup] {
	return flow.Retry(flow.Named("GrantReadOnly:"+username+"@"+database,
		func(ctx context.Context, setup *ServiceDbSetup) error {
			// Would execute: GRANT SELECT ON database.* TO 'username'@'%'
			return nil
		}))
}

func SetBinlogRetention(hours int) flow.Step[*ServiceDbSetup] {
	return flow.AutoNamed(flow.Retry(
		func(ctx context.Context, setup *ServiceDbSetup) error {
			// Would set binlog retention configuration
			return nil
		}))
}

// Retry predicates

func isTransientAWSError(err error) bool {
	// Stub: In real implementation, would check for specific AWS error types
	// like throttling, rate limiting, temporary service unavailability, etc.
	return true
}

func main() {
	// Example: Run the environment setup with default services configuration
	config := EnvironmentConfig{
		Name:   "production",
		Region: "us-west-2",
	}

	services := DefaultServicesConfig()
	setup := NewEnvironmentSetup(config, &services)

	ctx := context.Background()
	if err := setup.Run(ctx); err != nil {
		panic(err)
	}
}

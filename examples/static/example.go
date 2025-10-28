// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"time"

	"github.com/sam-fredrickson/flow"
)

// DatabaseSpec represents the configuration state for a database instance.
type DatabaseSpec struct {
	Name            string
	Databases       []string
	Users           []string
	Grants          map[string][]string
	BinlogRetention int
}

// ProvisionDatabase demonstrates using the flow library to declaratively
// specify database provisioning logic.
//
// This example shows how complex provisioning workflows can be expressed
// as a composition of steps, replacing nested if-statements and imperative
// code with a clear, declarative specification.
var ProvisionDatabase = flow.Do(
	// Initial setup and parallel database creation
	CreateDatabaseInstance(),
	ConfigureNetworking(),
	flow.InParallel(
		flow.Steps(
			CreateDatabase("app"),
			CreateDatabase("analytics"),
			CreateDatabase("cache"),
		),
	),

	// Create standard users with their databases
	// Uses InSerial with ForEach since we generate multiple steps per user
	flow.InSerial(
		flow.ForEach(
			func(_ context.Context, spec *DatabaseSpec) ([]string, error) {
				return []string{"app_user", "analytics_user", "readonly_user"}, nil
			},
			func(username string) flow.Step[*DatabaseSpec] {
				return flow.Do(
					CreateUser(username),
					GrantDatabaseAccess(username, username+"_db"),
				)
			},
		),
	),

	// Special users: conditional replication, admin with retry, monitoring
	// best-effort, and final validation
	flow.When(
		IsReplica(),
		flow.Do(
			CreateUser("replicator"),
			SetBinlogRetention(72),
			GrantReplicationPrivileges("replicator"),
			GrantGlobalReadAccess("replicator"),
		),
	),
	flow.Retry(
		flow.Do(
			CreateUser("admin"),
			GrantAllPrivileges("admin"),
		),
		flow.UpTo(3),
		flow.ExponentialBackoff(100*time.Millisecond),
	),
	flow.IgnoreError(
		flow.Do(
			CreateUser("monitor"),
			GrantMetricsAccess("monitor"),
		),
	),
	ValidateConfiguration(),
)

// Step constructors - these return flow.Step[*DatabaseSpec]
// Actual implementations would interact with database APIs

func CreateDatabaseInstance() flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would call cloud provider API to create database instance
		return nil
	}
}

func ConfigureNetworking() flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would configure VPC, security groups, etc.
		return nil
	}
}

func CreateDatabase(name string) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would execute: CREATE DATABASE name
		spec.Databases = append(spec.Databases, name)
		return nil
	}
}

func CreateUser(username string) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would execute: CREATE USER username
		spec.Users = append(spec.Users, username)
		return nil
	}
}

func GrantDatabaseAccess(username, database string) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would execute: GRANT ALL ON database.* TO username
		if spec.Grants == nil {
			spec.Grants = make(map[string][]string)
		}
		spec.Grants[username] = append(spec.Grants[username], database)
		return nil
	}
}

func SetBinlogRetention(hours int) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would configure binlog retention policy
		spec.BinlogRetention = hours
		return nil
	}
}

func GrantReplicationPrivileges(username string) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would execute: GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO username
		return nil
	}
}

func GrantGlobalReadAccess(username string) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would execute: GRANT SELECT ON *.* TO username
		return nil
	}
}

func GrantAllPrivileges(username string) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would execute: GRANT ALL PRIVILEGES ON *.* TO username
		return nil
	}
}

func GrantMetricsAccess(username string) flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would grant access to performance_schema, information_schema, etc.
		return nil
	}
}

func ValidateConfiguration() flow.Step[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) error {
		// Would verify the database is properly configured
		return nil
	}
}

// Predicate constructors

func IsReplica() flow.Predicate[*DatabaseSpec] {
	return func(ctx context.Context, spec *DatabaseSpec) (bool, error) {
		// Would check if this instance is configured as a replica
		return false, nil
	}
}

func main() {
	// Example: Run the database provisioning workflow
	spec := &DatabaseSpec{
		Name: "production-db",
	}

	ctx := context.Background()
	if err := ProvisionDatabase(ctx, spec); err != nil {
		panic(err)
	}
}

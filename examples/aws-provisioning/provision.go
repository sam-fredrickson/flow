// Package main demonstrates a realistic AWS infrastructure provisioning
// workflow using the flow library with an in-process fake AWS backend.
//
// No Docker, no AWS account, no LocalStack. All AWS API calls are intercepted
// at the Smithy middleware layer and dispatched to stateful in-process fakes.
package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/sam-fredrickson/flow"

	"github.com/sam-fredrickson/flow/examples/aws-provisioning/awsfake"
)

// EnvConfig holds the configuration for the provisioning workflow.
type EnvConfig struct {
	Name   string
	Region string
	AZs    []string

	VpcCIDR    string
	SubnetCIDR []string // one per AZ

	DBInstances []DBInstanceSpec

	// Services declares each service's database setup needs.
	// The config-driven pattern: services embed flow.Steps directly,
	// making the Go code itself an executable spec.
	Services map[string]ServiceConfig
}

// DBInstanceSpec describes a database instance to provision.
type DBInstanceSpec struct {
	Identifier string
	Engine     string
	Class      string
	StorageGB  int32
	MultiAZ    bool
}

// ServiceConfig declares a service's database needs.
type ServiceConfig struct {
	Databases []ServiceDbConfig
}

// ServiceDbConfig pairs a DB instance with the setup steps to run on it.
type ServiceDbConfig struct {
	Instance string
	Setup    flow.Step[*DBSetupContext]
}

// Env holds the workflow state, AWS clients, and accumulated resource IDs.
type Env struct {
	Config EnvConfig

	// AWS clients (wired to fake backend)
	EC2 *ec2.Client
	RDS *rds.Client
	IAM *iam.Client

	// Accumulated resource IDs (protected by mu for parallel access)
	mu                  sync.Mutex
	VpcID               string
	SubnetIDs           []string
	SecurityGroupID     string
	MonitoringRoleARN   string
	MonitoringPolicyARN string
	DBInstances         map[string]string // identifier → ARN
}

// GetSubnetIDs safely gets the subnet IDs from the Env.
func (e *Env) GetSubnetIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.SubnetIDs...)
}

// Provision is the top-level workflow that composes all phases.
func Provision() flow.Step[*Env] {
	return flow.Do(
		SetupNetwork(),
		SetupIAM(),
		CreateDatabases(),
		ConfigureDatabases(),
		Validate(),
	)
}

// awsStep wraps a leaf step with AutoNamed + Retry, giving every
// AWS-calling step standard retry behavior and automatic naming.
func awsStep[T any](step flow.Step[T]) flow.Step[T] {
	return flow.AutoNamed(flow.Retry(step, flow.UpTo(6)), flow.SkipCaller(1))
}

func main() {
	// Set up fake backend with stochastic fault injection.
	backend := awsfake.New("us-west-2")

	backend.InjectFault("CreateVpc", fmt.Errorf("RequestLimitExceeded"), 0.3)
	backend.InjectFault("CreateSubnet", fmt.Errorf("RequestLimitExceeded"), 0.3)
	backend.InjectFault("CreateSecurityGroup", fmt.Errorf("RequestLimitExceeded"), 0.2)
	backend.InjectFault("AuthorizeSecurityGroupIngress", fmt.Errorf("throttling: rate exceeded"), 0.2)
	backend.InjectFault("CreatePolicy", fmt.Errorf("throttling: rate exceeded"), 0.3)
	backend.InjectFault("CreateRole", fmt.Errorf("throttling: rate exceeded"), 0.3)
	backend.InjectFault("AttachRolePolicy", fmt.Errorf("throttling: rate exceeded"), 0.2)
	backend.InjectFault("CreateDBSubnetGroup", fmt.Errorf("throttling: rate exceeded"), 0.3)
	backend.InjectFault("CreateDBInstance", fmt.Errorf("throttling: rate exceeded"), 0.4)
	backend.InjectFault("DescribeDBInstances", fmt.Errorf("InternalError"), 0.1)

	cfg := backend.Config()

	env := &Env{
		Config:      DefaultConfig(),
		EC2:         ec2.NewFromConfig(cfg),
		RDS:         rds.NewFromConfig(cfg),
		IAM:         iam.NewFromConfig(cfg),
		DBInstances: make(map[string]string),
	}

	ctx := context.Background()

	// Run with tracing, output the trace tree.
	fmt.Println("=== AWS Provisioning Demo ===")
	fmt.Println("Running infrastructure provisioning workflow...")
	fmt.Println()

	err := flow.Spawn(
		flow.Traced(Provision()),
		flow.WriteFlatTextTo(os.Stdout),
	)(ctx, env)

	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nProvisioning complete!")
}

// DefaultConfig returns a realistic configuration for the demo.
func DefaultConfig() EnvConfig {
	return EnvConfig{
		Name:   "production",
		Region: "us-west-2",
		AZs:    []string{"us-west-2a", "us-west-2b", "us-west-2c"},

		VpcCIDR:    "10.0.0.0/16",
		SubnetCIDR: []string{"10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"},

		DBInstances: []DBInstanceSpec{
			{Identifier: "main-db", Engine: "postgres", Class: "db.r6g.xlarge", StorageGB: 100, MultiAZ: true},
			{Identifier: "analytics-db", Engine: "postgres", Class: "db.r6g.large", StorageGB: 200, MultiAZ: false},
		},

		Services: map[string]ServiceConfig{
			"webapp": {
				Databases: []ServiceDbConfig{{
					Instance: "main-db",
					Setup: flow.Do(
						CreateDatabase("webapp_db"),
						CreateUser("webapp_user"),
						GrantReadWrite("webapp_user", "webapp_db"),
					),
				}},
			},
			"api": {
				Databases: []ServiceDbConfig{{
					Instance: "main-db",
					Setup: flow.Do(
						CreateDatabase("api_db"),
						CreateUser("api_user"),
						GrantReadWrite("api_user", "api_db"),
					),
				}},
			},
			"analytics": {
				Databases: []ServiceDbConfig{{
					Instance: "analytics-db",
					Setup: flow.Do(
						CreateDatabase("analytics_db"),
						CreateUser("analytics_user"),
						GrantReadWrite("analytics_user", "analytics_db"),
					),
				}},
			},
			"reporting": {
				Databases: []ServiceDbConfig{{
					Instance: "analytics-db",
					Setup: flow.Do(
						CreateDatabase("reporting_db"),
						CreateUser("reporting_user"),
						GrantReadWrite("reporting_user", "reporting_db"),
						GrantReadOnly("reporting_user", "analytics_db"),
					),
				}},
			},
			"background-jobs": {
				Databases: []ServiceDbConfig{
					{
						Instance: "main-db",
						Setup: flow.Do(
							CreateDatabase("jobs_db"),
							CreateUser("jobs_user"),
							GrantReadWrite("jobs_user", "jobs_db"),
							flow.IgnoreError(CreateMonitoringUser("jobs_db")),
						),
					},
					{
						Instance: "analytics-db",
						Setup: flow.Do(
							CreateUser("jobs_reader"),
							GrantReadOnly("jobs_reader", "analytics_db"),
						),
					},
				},
			},
		},
	}
}

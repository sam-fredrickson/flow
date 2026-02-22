package main

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/sam-fredrickson/flow"
)

// CreateDatabases provisions all configured DB instances in parallel.
// Each instance goes through: create subnet group → create instance (with retry) → wait for ready.
func CreateDatabases() flow.Step[*Env] {
	return flow.AutoNamed(flow.InParallel(flow.ForEach(
		func(_ context.Context, env *Env) ([]DBInstanceSpec, error) {
			return env.Config.DBInstances, nil
		},
		createDBInstance,
	)))
}

func createDBInstance(spec DBInstanceSpec) flow.Step[*Env] {
	return flow.Named(spec.Identifier, flow.Do(
		createDBSubnetGroup(spec),
		flow.Retry(createInstance(spec),
			flow.UpTo(5),
			flow.ExponentialBackoff(50*time.Millisecond, flow.WithFullJitter()),
		),
		flow.While(
			instanceNotReady(spec.Identifier),
			flow.Do(
				flow.Sleep[*Env](10*time.Millisecond),
				describeInstance(spec.Identifier),
			),
		),
	))
}

func createDBSubnetGroup(spec DBInstanceSpec) flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		subnetIDs := env.GetSubnetIDs()
		_, err := env.RDS.CreateDBSubnetGroup(ctx, &rds.CreateDBSubnetGroupInput{
			DBSubnetGroupName:        aws.String(spec.Identifier + "-subnets"),
			DBSubnetGroupDescription: aws.String("Subnet group for " + spec.Identifier),
			SubnetIds:                subnetIDs,
		})
		return err
	})
}

func createInstance(spec DBInstanceSpec) flow.Step[*Env] {
	return flow.AutoNamed(func(ctx context.Context, env *Env) error {
		_, err := env.RDS.CreateDBInstance(ctx, &rds.CreateDBInstanceInput{
			DBInstanceIdentifier:  aws.String(spec.Identifier),
			DBInstanceClass:       aws.String(spec.Class),
			Engine:                aws.String(spec.Engine),
			AllocatedStorage:      aws.Int32(spec.StorageGB),
			MultiAZ:               aws.Bool(spec.MultiAZ),
			MasterUsername:        aws.String("admin"),
			MasterUserPassword:    aws.String("changeme-in-production"),
			DBSubnetGroupName:     aws.String(spec.Identifier + "-subnets"),
			VpcSecurityGroupIds:   []string{env.SecurityGroupID},
			MonitoringRoleArn:     aws.String(env.MonitoringRoleARN),
			StorageType:           aws.String("gp3"),
			StorageEncrypted:      aws.Bool(true),
			BackupRetentionPeriod: aws.Int32(7),
		})
		return err
	})
}

func instanceNotReady(identifier string) flow.Predicate[*Env] {
	return func(ctx context.Context, env *Env) (bool, error) {
		env.mu.Lock()
		_, ready := env.DBInstances[identifier]
		env.mu.Unlock()
		return !ready, nil
	}
}

func describeInstance(identifier string) flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.RDS.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			DBInstanceIdentifier: aws.String(identifier),
		})
		if err != nil {
			return err
		}
		if len(out.DBInstances) > 0 && aws.ToString(out.DBInstances[0].DBInstanceStatus) == "available" {
			env.mu.Lock()
			env.DBInstances[identifier] = aws.ToString(out.DBInstances[0].DBInstanceArn)
			env.mu.Unlock()
		}
		return nil
	})
}

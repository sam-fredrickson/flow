package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/sam-fredrickson/flow"
)

// Validate verifies that all provisioned resources exist and are healthy.
func Validate() flow.Step[*Env] {
	return flow.AutoNamed(flow.Do(
		verifyVPC(),
		verifySubnets(),
		verifyDatabases(),
	))
}

func verifyVPC() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.EC2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
			VpcIds: []string{env.VpcID},
		})
		if err != nil {
			return fmt.Errorf("describe VPC: %w", err)
		}
		if len(out.Vpcs) != 1 {
			return fmt.Errorf("expected 1 VPC, got %d", len(out.Vpcs))
		}
		return nil
	})
}

func verifySubnets() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.EC2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("vpc-id"), Values: []string{env.VpcID}},
			},
		})
		if err != nil {
			return fmt.Errorf("describe subnets: %w", err)
		}
		expected := len(env.Config.AZs)
		if len(out.Subnets) != expected {
			return fmt.Errorf("expected %d subnets, got %d", expected, len(out.Subnets))
		}
		return nil
	})
}

func verifyDatabases() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.RDS.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{})
		if err != nil {
			return fmt.Errorf("describe DB instances: %w", err)
		}
		for _, inst := range out.DBInstances {
			status := aws.ToString(inst.DBInstanceStatus)
			if status != "available" {
				return fmt.Errorf("instance %s has status %q, expected available",
					aws.ToString(inst.DBInstanceIdentifier), status)
			}
		}
		return nil
	})
}

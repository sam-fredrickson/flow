package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/sam-fredrickson/flow"
)

// SetupIAM creates IAM roles and policies for RDS enhanced monitoring.
func SetupIAM() flow.Step[*Env] {
	return flow.AutoNamed(flow.Do(
		createMonitoringPolicy(),
		createMonitoringRole(),
		attachMonitoringPolicy(),
	))
}

func createMonitoringPolicy() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.IAM.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyName: aws.String(env.Config.Name + "-rds-monitoring"),
			PolicyDocument: aws.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Action": [
						"logs:CreateLogGroup",
						"logs:CreateLogStream",
						"logs:PutLogEvents",
						"logs:DescribeLogStreams"
					],
					"Resource": "*"
				}]
			}`),
		})
		if err != nil {
			return err
		}
		env.MonitoringPolicyARN = aws.ToString(out.Policy.Arn)
		return nil
	})
}

func createMonitoringRole() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.IAM.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:    aws.String(env.Config.Name + "-rds-monitoring-role"),
			Description: aws.String("Role for RDS enhanced monitoring"),
			AssumeRolePolicyDocument: aws.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Principal": {"Service": "monitoring.rds.amazonaws.com"},
					"Action": "sts:AssumeRole"
				}]
			}`),
		})
		if err != nil {
			return err
		}
		env.MonitoringRoleARN = aws.ToString(out.Role.Arn)
		return nil
	})
}

func attachMonitoringPolicy() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		_, err := env.IAM.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  aws.String(env.Config.Name + "-rds-monitoring-role"),
			PolicyArn: aws.String(env.MonitoringPolicyARN),
		})
		return err
	})
}

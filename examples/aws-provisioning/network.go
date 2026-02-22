package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/sam-fredrickson/flow"
)

// SetupNetwork creates the VPC, subnets, and security groups.
func SetupNetwork() flow.Step[*Env] {
	return flow.AutoNamed(flow.Do(
		createVpc(),
		createSubnets(),
		createSecurityGroup(),
		authorizeDBIngress(),
	))
}

func createVpc() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.EC2.CreateVpc(ctx, &ec2.CreateVpcInput{
			CidrBlock: aws.String(env.Config.VpcCIDR),
		})
		if err != nil {
			return err
		}
		env.VpcID = aws.ToString(out.Vpc.VpcId)
		return nil
	})
}

// subnetSpec pairs an availability zone with its CIDR block.
type subnetSpec struct {
	AZ   string
	CIDR string
}

func createSubnets() flow.Step[*Env] {
	// Create one subnet per AZ, in parallel.
	return flow.AutoNamed(flow.InParallel(flow.ForEach(
		func(_ context.Context, env *Env) ([]subnetSpec, error) {
			specs := make([]subnetSpec, len(env.Config.AZs))
			for i, az := range env.Config.AZs {
				specs[i] = subnetSpec{AZ: az, CIDR: env.Config.SubnetCIDR[i]}
			}
			return specs, nil
		},
		func(spec subnetSpec) flow.Step[*Env] {
			return flow.Named(
				spec.AZ,
				flow.Retry(func(ctx context.Context, env *Env) error {
					out, err := env.EC2.CreateSubnet(ctx, &ec2.CreateSubnetInput{
						VpcId:            aws.String(env.VpcID),
						CidrBlock:        aws.String(spec.CIDR),
						AvailabilityZone: aws.String(spec.AZ),
					})
					if err != nil {
						return err
					}
					subnetID := aws.ToString(out.Subnet.SubnetId)
					env.mu.Lock()
					env.SubnetIDs = append(env.SubnetIDs, subnetID)
					env.mu.Unlock()
					return nil
				}),
			)
		},
	)))
}

func createSecurityGroup() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		out, err := env.EC2.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
			GroupName:   aws.String(env.Config.Name + "-db-sg"),
			Description: aws.String("Security group for database access"),
			VpcId:       aws.String(env.VpcID),
		})
		if err != nil {
			return err
		}
		env.SecurityGroupID = aws.ToString(out.GroupId)
		return nil
	})
}

func authorizeDBIngress() flow.Step[*Env] {
	return awsStep(func(ctx context.Context, env *Env) error {
		_, err := env.EC2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
			GroupId: aws.String(env.SecurityGroupID),
			IpPermissions: []ec2types.IpPermission{
				{
					IpProtocol: aws.String("tcp"),
					FromPort:   aws.Int32(5432),
					ToPort:     aws.Int32(5432),
					IpRanges: []ec2types.IpRange{
						{CidrIp: aws.String(env.Config.VpcCIDR), Description: aws.String("PostgreSQL from VPC")},
					},
				},
				{
					IpProtocol: aws.String("tcp"),
					FromPort:   aws.Int32(3306),
					ToPort:     aws.Int32(3306),
					IpRanges: []ec2types.IpRange{
						{CidrIp: aws.String(env.Config.VpcCIDR), Description: aws.String("MySQL from VPC")},
					},
				},
			},
		})
		return err
	})
}

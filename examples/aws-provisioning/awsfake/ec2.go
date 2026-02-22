package awsfake

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2Fake is a stateful in-process fake for EC2/VPC operations.
type EC2Fake struct {
	mu             sync.Mutex
	vpcs           map[string]ec2types.Vpc
	subnets        map[string]ec2types.Subnet
	securityGroups map[string]ec2types.SecurityGroup
}

// NewEC2Fake creates a new EC2 fake with empty state.
func NewEC2Fake() *EC2Fake {
	return &EC2Fake{
		vpcs:           make(map[string]ec2types.Vpc),
		subnets:        make(map[string]ec2types.Subnet),
		securityGroups: make(map[string]ec2types.SecurityGroup),
	}
}

func (f *EC2Fake) CreateVpc(_ context.Context, params any) (*ec2.CreateVpcOutput, error) {
	input, ok := params.(*ec2.CreateVpcInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *ec2.CreateVpcInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	vpcID := RandomID("vpc", 8)
	vpc := ec2types.Vpc{
		VpcId:     aws.String(vpcID),
		CidrBlock: input.CidrBlock,
		State:     ec2types.VpcStateAvailable,
	}
	f.vpcs[vpcID] = vpc

	return &ec2.CreateVpcOutput{Vpc: &vpc}, nil
}

func (f *EC2Fake) CreateSubnet(_ context.Context, params any) (*ec2.CreateSubnetOutput, error) {
	input, ok := params.(*ec2.CreateSubnetInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *ec2.CreateSubnetInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	subnetID := RandomID("subnet", 8)
	subnet := ec2types.Subnet{
		SubnetId:         aws.String(subnetID),
		VpcId:            input.VpcId,
		CidrBlock:        input.CidrBlock,
		AvailabilityZone: input.AvailabilityZone,
		State:            ec2types.SubnetStateAvailable,
	}
	f.subnets[subnetID] = subnet

	return &ec2.CreateSubnetOutput{Subnet: &subnet}, nil
}

func (f *EC2Fake) CreateSecurityGroup(_ context.Context, params any) (*ec2.CreateSecurityGroupOutput, error) {
	input, ok := params.(*ec2.CreateSecurityGroupInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *ec2.CreateSecurityGroupInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	sgID := RandomID("sg", 8)
	sg := ec2types.SecurityGroup{
		GroupId:     aws.String(sgID),
		GroupName:   input.GroupName,
		Description: input.Description,
		VpcId:       input.VpcId,
	}
	f.securityGroups[sgID] = sg

	return &ec2.CreateSecurityGroupOutput{GroupId: aws.String(sgID)}, nil
}

func (f *EC2Fake) AuthorizeSecurityGroupIngress(_ context.Context, params any) (*ec2.AuthorizeSecurityGroupIngressOutput, error) {
	_, ok := params.(*ec2.AuthorizeSecurityGroupIngressInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *ec2.AuthorizeSecurityGroupIngressInput, got %T", params)
	}

	// We don't track ingress rules in detail — just accept the call.
	return &ec2.AuthorizeSecurityGroupIngressOutput{Return: aws.Bool(true)}, nil
}

func (f *EC2Fake) DescribeVpcs(_ context.Context, params any) (*ec2.DescribeVpcsOutput, error) {
	input, ok := params.(*ec2.DescribeVpcsInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *ec2.DescribeVpcsInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var vpcs []ec2types.Vpc
	if len(input.VpcIds) > 0 {
		for _, id := range input.VpcIds {
			if vpc, ok := f.vpcs[id]; ok {
				vpcs = append(vpcs, vpc)
			}
		}
	} else {
		for _, vpc := range f.vpcs {
			vpcs = append(vpcs, vpc)
		}
	}

	return &ec2.DescribeVpcsOutput{Vpcs: vpcs}, nil
}

func (f *EC2Fake) DescribeSubnets(_ context.Context, params any) (*ec2.DescribeSubnetsOutput, error) {
	input, ok := params.(*ec2.DescribeSubnetsInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *ec2.DescribeSubnetsInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var subnets []ec2types.Subnet

	// Check for VPC filter.
	var filterVpcID string
	for _, filter := range input.Filters {
		if aws.ToString(filter.Name) == "vpc-id" && len(filter.Values) > 0 {
			filterVpcID = filter.Values[0]
		}
	}

	for _, subnet := range f.subnets {
		if filterVpcID != "" && aws.ToString(subnet.VpcId) != filterVpcID {
			continue
		}
		subnets = append(subnets, subnet)
	}

	return &ec2.DescribeSubnetsOutput{Subnets: subnets}, nil
}

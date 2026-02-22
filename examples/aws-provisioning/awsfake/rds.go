package awsfake

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// RDSFake is a stateful in-process fake for RDS operations.
// It tracks describe-call counts to simulate "creating" → "available" promotion.
type RDSFake struct {
	backend        *Backend
	mu             sync.Mutex
	instances      map[string]*rdstypes.DBInstance
	subnetGroups   map[string]*rdstypes.DBSubnetGroup
	describeCounts map[string]int // tracks calls per instance for status promotion
}

// NewRDSFake creates a new RDS fake with empty state.
func NewRDSFake(backend *Backend) *RDSFake {
	return &RDSFake{
		backend:        backend,
		instances:      make(map[string]*rdstypes.DBInstance),
		subnetGroups:   make(map[string]*rdstypes.DBSubnetGroup),
		describeCounts: make(map[string]int),
	}
}

func (f *RDSFake) CreateDBInstance(_ context.Context, params any) (*rds.CreateDBInstanceOutput, error) {
	input, ok := params.(*rds.CreateDBInstanceInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *rds.CreateDBInstanceInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	id := aws.ToString(input.DBInstanceIdentifier)
	arn := f.backend.ARN("rds", "db", id)

	instance := &rdstypes.DBInstance{
		DBInstanceIdentifier: input.DBInstanceIdentifier,
		DBInstanceClass:      input.DBInstanceClass,
		Engine:               input.Engine,
		DBInstanceArn:        aws.String(arn),
		DBInstanceStatus:     aws.String("creating"),
		DBSubnetGroup: &rdstypes.DBSubnetGroup{
			DBSubnetGroupName: input.DBSubnetGroupName,
		},
		AvailabilityZone:      input.AvailabilityZone,
		MultiAZ:               input.MultiAZ,
		AllocatedStorage:      input.AllocatedStorage,
		MasterUsername:        input.MasterUsername,
		MonitoringRoleArn:     input.MonitoringRoleArn,
		VpcSecurityGroups:     makeVpcSGMemberships(input.VpcSecurityGroupIds),
		StorageType:           input.StorageType,
		StorageEncrypted:      input.StorageEncrypted,
		BackupRetentionPeriod: input.BackupRetentionPeriod,
	}
	f.instances[id] = instance

	return &rds.CreateDBInstanceOutput{DBInstance: instance}, nil
}

func (f *RDSFake) DescribeDBInstances(_ context.Context, params any) (*rds.DescribeDBInstancesOutput, error) {
	input, ok := params.(*rds.DescribeDBInstancesInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *rds.DescribeDBInstancesInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var instances []rdstypes.DBInstance

	if input.DBInstanceIdentifier != nil {
		id := aws.ToString(input.DBInstanceIdentifier)
		inst, exists := f.instances[id]
		if !exists {
			return nil, fmt.Errorf("DBInstance %q not found", id)
		}

		// Track describe count and promote after 3 calls.
		f.describeCounts[id]++
		if f.describeCounts[id] >= 3 && aws.ToString(inst.DBInstanceStatus) == "creating" {
			inst.DBInstanceStatus = aws.String("available")
		}

		instances = append(instances, *inst)
	} else {
		for id, inst := range f.instances {
			f.describeCounts[id]++
			if f.describeCounts[id] >= 3 && aws.ToString(inst.DBInstanceStatus) == "creating" {
				inst.DBInstanceStatus = aws.String("available")
			}
			instances = append(instances, *inst)
		}
	}

	return &rds.DescribeDBInstancesOutput{DBInstances: instances}, nil
}

func (f *RDSFake) CreateDBSubnetGroup(_ context.Context, params any) (*rds.CreateDBSubnetGroupOutput, error) {
	input, ok := params.(*rds.CreateDBSubnetGroupInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *rds.CreateDBSubnetGroupInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	name := aws.ToString(input.DBSubnetGroupName)
	arn := f.backend.ARN("rds", "subgrp", name)

	var subnets []rdstypes.Subnet
	for _, subnetID := range input.SubnetIds {
		subnets = append(subnets, rdstypes.Subnet{
			SubnetIdentifier: aws.String(subnetID),
		})
	}

	group := &rdstypes.DBSubnetGroup{
		DBSubnetGroupName:        input.DBSubnetGroupName,
		DBSubnetGroupDescription: input.DBSubnetGroupDescription,
		DBSubnetGroupArn:         aws.String(arn),
		Subnets:                  subnets,
	}
	f.subnetGroups[name] = group

	return &rds.CreateDBSubnetGroupOutput{DBSubnetGroup: group}, nil
}

func makeVpcSGMemberships(ids []string) []rdstypes.VpcSecurityGroupMembership {
	var memberships []rdstypes.VpcSecurityGroupMembership
	for _, id := range ids {
		memberships = append(memberships, rdstypes.VpcSecurityGroupMembership{
			VpcSecurityGroupId: aws.String(id),
			Status:             aws.String("active"),
		})
	}
	return memberships
}

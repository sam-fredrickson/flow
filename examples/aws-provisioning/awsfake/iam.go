package awsfake

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// IAMFake is a stateful in-process fake for IAM operations.
// It provides just enough for RDS enhanced monitoring role setup.
type IAMFake struct {
	backend  *Backend
	mu       sync.Mutex
	roles    map[string]*iamtypes.Role
	policies map[string]*iamtypes.Policy
}

// NewIAMFake creates a new IAM fake with empty state.
func NewIAMFake(backend *Backend) *IAMFake {
	return &IAMFake{
		backend:  backend,
		roles:    make(map[string]*iamtypes.Role),
		policies: make(map[string]*iamtypes.Policy),
	}
}

func (f *IAMFake) CreateRole(_ context.Context, params any) (*iam.CreateRoleOutput, error) {
	input, ok := params.(*iam.CreateRoleInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *iam.CreateRoleInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	name := aws.ToString(input.RoleName)
	arn := f.backend.ARN("iam", "role", name)

	role := &iamtypes.Role{
		RoleName:                 input.RoleName,
		Arn:                      aws.String(arn),
		AssumeRolePolicyDocument: input.AssumeRolePolicyDocument,
		Description:              input.Description,
		Path:                     input.Path,
	}
	f.roles[name] = role

	return &iam.CreateRoleOutput{Role: role}, nil
}

func (f *IAMFake) CreatePolicy(_ context.Context, params any) (*iam.CreatePolicyOutput, error) {
	input, ok := params.(*iam.CreatePolicyInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *iam.CreatePolicyInput, got %T", params)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	name := aws.ToString(input.PolicyName)
	arn := f.backend.ARN("iam", "policy", name)

	policy := &iamtypes.Policy{
		PolicyName: input.PolicyName,
		Arn:        aws.String(arn),
	}
	f.policies[name] = policy

	return &iam.CreatePolicyOutput{Policy: policy}, nil
}

func (f *IAMFake) AttachRolePolicy(_ context.Context, params any) (*iam.AttachRolePolicyOutput, error) {
	_, ok := params.(*iam.AttachRolePolicyInput)
	if !ok {
		return nil, fmt.Errorf("awsfake: expected *iam.AttachRolePolicyInput, got %T", params)
	}

	// Accept the call without tracking — sufficient for this demo.
	return &iam.AttachRolePolicyOutput{}, nil
}

module github.com/sam-fredrickson/flow/examples/aws-provisioning

go 1.27.0

require (
	github.com/aws/aws-sdk-go-v2 v1.47.5
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.343.0
	github.com/aws/aws-sdk-go-v2/service/iam v1.64.6
	github.com/aws/aws-sdk-go-v2/service/rds v1.131.2
	github.com/aws/smithy-go v1.28.7
	github.com/sam-fredrickson/flow v0.3.0
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.8 // indirect
	golang.org/x/sync v0.24.0 // indirect
)

replace github.com/sam-fredrickson/flow => ../..

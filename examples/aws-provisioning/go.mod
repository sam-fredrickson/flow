module github.com/sam-fredrickson/flow/examples/aws-provisioning

go 1.24.0

require (
	github.com/aws/aws-sdk-go-v2 v1.36.5
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.210.0
	github.com/aws/aws-sdk-go-v2/service/iam v1.42.0
	github.com/aws/aws-sdk-go-v2/service/rds v1.98.0
	github.com/aws/smithy-go v1.22.4
	github.com/sam-fredrickson/flow v0.0.0
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.36 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.36 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.17 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

replace github.com/sam-fredrickson/flow => ../..

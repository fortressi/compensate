module github.com/fortressi/compensate/examples/aws-infrastructure

go 1.23

toolchain go1.24.3

require (
	github.com/aws/aws-sdk-go-v2 v1.24.0
	github.com/aws/aws-sdk-go-v2/config v1.26.1
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.142.0
	github.com/fortressi/compensate v0.0.0
	github.com/google/uuid v1.6.0
)

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.16.12 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.14.10 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.2.9 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.5.9 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.7.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.10.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.10.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.18.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.21.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.26.5 // indirect
	github.com/aws/smithy-go v1.19.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.4.0 // indirect
	github.com/tidwall/btree v1.7.0 // indirect
	gonum.org/v1/gonum v0.15.1 // indirect
)

replace github.com/fortressi/compensate => ../../

package acm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
)

// ClientFactory creates ACM clients for a given region.
// This allows the controller to lazily create per-region clients.
type ClientFactory interface {
	ClientForRegion(ctx context.Context, region string) (Client, error)
}

// SDKClientFactory creates real ACM clients using the AWS SDK.
type SDKClientFactory struct {
	UseFIPS bool
}

func (f *SDKClientFactory) ClientForRegion(ctx context.Context, region string) (Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if f.UseFIPS {
		opts = append(opts, awsconfig.WithUseFIPSEndpoint(aws.FIPSEndpointStateEnabled))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for region %s: %w", region, err)
	}

	api := awsacm.NewFromConfig(cfg)
	return NewRealClient(api), nil
}

// MockClientFactory returns a preconfigured mock client for all regions.
type MockClientFactory struct {
	Mock *MockClient
}

func (f *MockClientFactory) ClientForRegion(_ context.Context, _ string) (Client, error) {
	return f.Mock, nil
}

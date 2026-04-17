package acm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
)

// RealClient implements Client using the AWS ACM SDK.
type RealClient struct {
	api ACMAPI
}

// NewRealClient creates a Client backed by the given ACMAPI.
func NewRealClient(api ACMAPI) *RealClient {
	return &RealClient{api: api}
}

func (c *RealClient) ImportCertificate(ctx context.Context, arn string, cert, key, chain []byte) (string, error) {
	input := &awsacm.ImportCertificateInput{
		Certificate: cert,
		PrivateKey:  key,
	}
	if len(chain) > 0 {
		input.CertificateChain = chain
	}
	if arn != "" {
		input.CertificateArn = aws.String(arn)
	}

	out, err := c.api.ImportCertificate(ctx, input)
	if err != nil {
		return "", fmt.Errorf("acm ImportCertificate: %w", err)
	}
	if out.CertificateArn == nil {
		return "", fmt.Errorf("acm ImportCertificate returned nil ARN")
	}
	return *out.CertificateArn, nil
}

func (c *RealClient) AddTags(ctx context.Context, arn string, tags map[string]string) error {
	if len(tags) == 0 {
		return nil
	}

	acmTags := make([]acmtypes.Tag, 0, len(tags))
	for k, v := range tags {
		acmTags = append(acmTags, acmtypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err := c.api.AddTagsToCertificate(ctx, &awsacm.AddTagsToCertificateInput{
		CertificateArn: aws.String(arn),
		Tags:           acmTags,
	})
	if err != nil {
		return fmt.Errorf("acm AddTagsToCertificate: %w", err)
	}
	return nil
}

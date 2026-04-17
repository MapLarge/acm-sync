package acm

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/acm"
)

// ACMAPI defines the subset of the ACM SDK used by this controller.
type ACMAPI interface {
	ImportCertificate(ctx context.Context, params *acm.ImportCertificateInput, optFns ...func(*acm.Options)) (*acm.ImportCertificateOutput, error)
	AddTagsToCertificate(ctx context.Context, params *acm.AddTagsToCertificateInput, optFns ...func(*acm.Options)) (*acm.AddTagsToCertificateOutput, error)
	DescribeCertificate(ctx context.Context, params *acm.DescribeCertificateInput, optFns ...func(*acm.Options)) (*acm.DescribeCertificateOutput, error)
	ListTagsForCertificate(ctx context.Context, params *acm.ListTagsForCertificateInput, optFns ...func(*acm.Options)) (*acm.ListTagsForCertificateOutput, error)
}

// Client wraps the ACM SDK operations needed by the controller.
type Client interface {
	// ImportCertificate imports or re-imports a certificate into ACM.
	// If arn is empty, a new certificate is created and its ARN is returned.
	// If arn is non-empty, the existing certificate is updated in place.
	ImportCertificate(ctx context.Context, arn string, cert, key, chain []byte) (string, error)

	// AddTags applies tags to an ACM certificate.
	AddTags(ctx context.Context, arn string, tags map[string]string) error
}

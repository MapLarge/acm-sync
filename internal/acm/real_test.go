package acm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
)

// fakeACMAPI implements ACMAPI for testing the RealClient.
type fakeACMAPI struct {
	importFunc   func(ctx context.Context, params *awsacm.ImportCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.ImportCertificateOutput, error)
	addTagsFunc  func(ctx context.Context, params *awsacm.AddTagsToCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.AddTagsToCertificateOutput, error)
	describeFunc func(ctx context.Context, params *awsacm.DescribeCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.DescribeCertificateOutput, error)
	listTagsFunc func(ctx context.Context, params *awsacm.ListTagsForCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.ListTagsForCertificateOutput, error)
}

func (f *fakeACMAPI) ImportCertificate(ctx context.Context, params *awsacm.ImportCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.ImportCertificateOutput, error) {
	if f.importFunc != nil {
		return f.importFunc(ctx, params, optFns...)
	}
	return &awsacm.ImportCertificateOutput{
		CertificateArn: aws.String("arn:aws:acm:us-east-1:123456789012:certificate/new"),
	}, nil
}

func (f *fakeACMAPI) AddTagsToCertificate(ctx context.Context, params *awsacm.AddTagsToCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.AddTagsToCertificateOutput, error) {
	if f.addTagsFunc != nil {
		return f.addTagsFunc(ctx, params, optFns...)
	}
	return &awsacm.AddTagsToCertificateOutput{}, nil
}

func (f *fakeACMAPI) DescribeCertificate(ctx context.Context, params *awsacm.DescribeCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.DescribeCertificateOutput, error) {
	if f.describeFunc != nil {
		return f.describeFunc(ctx, params, optFns...)
	}
	return &awsacm.DescribeCertificateOutput{}, nil
}

func (f *fakeACMAPI) ListTagsForCertificate(ctx context.Context, params *awsacm.ListTagsForCertificateInput, optFns ...func(*awsacm.Options)) (*awsacm.ListTagsForCertificateOutput, error) {
	if f.listTagsFunc != nil {
		return f.listTagsFunc(ctx, params, optFns...)
	}
	return &awsacm.ListTagsForCertificateOutput{}, nil
}

func TestRealClient_ImportCertificate_NewCert(t *testing.T) {
	api := &fakeACMAPI{}
	client := NewRealClient(api)

	arn, err := client.ImportCertificate(context.Background(), "", []byte("cert"), []byte("key"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arn != "arn:aws:acm:us-east-1:123456789012:certificate/new" {
		t.Errorf("unexpected ARN: %s", arn)
	}
}

func TestRealClient_ImportCertificate_UpdateExisting(t *testing.T) {
	existingARN := "arn:aws:acm:us-east-1:123456789012:certificate/existing"
	var capturedInput *awsacm.ImportCertificateInput

	api := &fakeACMAPI{
		importFunc: func(_ context.Context, params *awsacm.ImportCertificateInput, _ ...func(*awsacm.Options)) (*awsacm.ImportCertificateOutput, error) {
			capturedInput = params
			return &awsacm.ImportCertificateOutput{
				CertificateArn: params.CertificateArn,
			}, nil
		},
	}
	client := NewRealClient(api)

	arn, err := client.ImportCertificate(context.Background(), existingARN, []byte("cert"), []byte("key"), []byte("chain"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arn != existingARN {
		t.Errorf("unexpected ARN: %s", arn)
	}
	if capturedInput.CertificateArn == nil || *capturedInput.CertificateArn != existingARN {
		t.Error("expected ARN to be passed to SDK")
	}
	if capturedInput.CertificateChain == nil {
		t.Error("expected chain to be passed to SDK")
	}
}

func TestRealClient_ImportCertificate_Error(t *testing.T) {
	api := &fakeACMAPI{
		importFunc: func(_ context.Context, _ *awsacm.ImportCertificateInput, _ ...func(*awsacm.Options)) (*awsacm.ImportCertificateOutput, error) {
			return nil, fmt.Errorf("throttled")
		},
	}
	client := NewRealClient(api)

	_, err := client.ImportCertificate(context.Background(), "", []byte("cert"), []byte("key"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "throttled") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRealClient_ImportCertificate_NilARNResponse(t *testing.T) {
	api := &fakeACMAPI{
		importFunc: func(_ context.Context, _ *awsacm.ImportCertificateInput, _ ...func(*awsacm.Options)) (*awsacm.ImportCertificateOutput, error) {
			return &awsacm.ImportCertificateOutput{CertificateArn: nil}, nil
		},
	}
	client := NewRealClient(api)

	_, err := client.ImportCertificate(context.Background(), "", []byte("cert"), []byte("key"), nil)
	if err == nil {
		t.Fatal("expected error for nil ARN")
	}
}

func TestRealClient_AddTags(t *testing.T) {
	var capturedInput *awsacm.AddTagsToCertificateInput

	api := &fakeACMAPI{
		addTagsFunc: func(_ context.Context, params *awsacm.AddTagsToCertificateInput, _ ...func(*awsacm.Options)) (*awsacm.AddTagsToCertificateOutput, error) {
			capturedInput = params
			return &awsacm.AddTagsToCertificateOutput{}, nil
		},
	}
	client := NewRealClient(api)

	tags := map[string]string{"env": "prod", "team": "platform"}
	err := client.AddTags(context.Background(), "arn:aws:acm:us-east-1:123:certificate/abc", tags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedInput.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(capturedInput.Tags))
	}

	tagMap := make(map[string]string)
	for _, tag := range capturedInput.Tags {
		tagMap[*tag.Key] = *tag.Value
	}
	for k, v := range tags {
		if tagMap[k] != v {
			t.Errorf("tag %q = %q, want %q", k, tagMap[k], v)
		}
	}
}

func TestRealClient_AddTags_EmptyTags(t *testing.T) {
	callCount := 0
	api := &fakeACMAPI{
		addTagsFunc: func(_ context.Context, _ *awsacm.AddTagsToCertificateInput, _ ...func(*awsacm.Options)) (*awsacm.AddTagsToCertificateOutput, error) {
			callCount++
			return &awsacm.AddTagsToCertificateOutput{}, nil
		},
	}
	client := NewRealClient(api)

	err := client.AddTags(context.Background(), "arn:aws:acm:us-east-1:123:certificate/abc", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Error("expected no SDK call for empty tags")
	}
}

func TestRealClient_AddTags_Error(t *testing.T) {
	api := &fakeACMAPI{
		addTagsFunc: func(_ context.Context, _ *awsacm.AddTagsToCertificateInput, _ ...func(*awsacm.Options)) (*awsacm.AddTagsToCertificateOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}
	client := NewRealClient(api)

	err := client.AddTags(context.Background(), "arn:aws:acm:us-east-1:123:certificate/abc", map[string]string{"k": "v"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Verify fakeACMAPI satisfies the interface at compile time.
var _ ACMAPI = (*fakeACMAPI)(nil)

// Verify the real SDK client satisfies ACMAPI.
var _ ACMAPI = (*awsacm.Client)(nil)

// Verify the type tags are correct for the acm types we use.
var _ acmtypes.Tag = acmtypes.Tag{Key: aws.String("k"), Value: aws.String("v")}

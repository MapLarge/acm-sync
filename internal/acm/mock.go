package acm

import (
	"context"
	"fmt"
	"sync"
)

// MockClient implements Client for testing.
type MockClient struct {
	mu sync.Mutex

	// ImportCertificateFunc can be set to override the default mock behavior.
	ImportCertificateFunc func(ctx context.Context, arn string, cert, key, chain []byte) (string, error)

	// AddTagsFunc can be set to override the default mock behavior.
	AddTagsFunc func(ctx context.Context, arn string, tags map[string]string) error

	// ImportCalls tracks calls to ImportCertificate.
	ImportCalls []ImportCall

	// TagCalls tracks calls to AddTags.
	TagCalls []TagCall

	// nextARN is returned by the default ImportCertificate implementation when no ARN is provided.
	nextARN string
}

// ImportCall records the arguments of an ImportCertificate call.
type ImportCall struct {
	ARN   string
	Cert  []byte
	Key   []byte
	Chain []byte
}

// TagCall records the arguments of an AddTags call.
type TagCall struct {
	ARN  string
	Tags map[string]string
}

// NewMockClient creates a MockClient that generates ARNs from the given base.
func NewMockClient(defaultARN string) *MockClient {
	return &MockClient{nextARN: defaultARN}
}

func (m *MockClient) ImportCertificate(ctx context.Context, arn string, cert, key, chain []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ImportCalls = append(m.ImportCalls, ImportCall{ARN: arn, Cert: cert, Key: key, Chain: chain})

	if m.ImportCertificateFunc != nil {
		return m.ImportCertificateFunc(ctx, arn, cert, key, chain)
	}

	if arn != "" {
		return arn, nil
	}
	if m.nextARN == "" {
		return "", fmt.Errorf("mock: no ARN configured for new import")
	}
	return m.nextARN, nil
}

func (m *MockClient) AddTags(ctx context.Context, arn string, tags map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TagCalls = append(m.TagCalls, TagCall{ARN: arn, Tags: tags})

	if m.AddTagsFunc != nil {
		return m.AddTagsFunc(ctx, arn, tags)
	}
	return nil
}

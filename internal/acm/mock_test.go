package acm

import (
	"context"
	"fmt"
	"testing"
)

// Verify MockClient satisfies Client.
var _ Client = (*MockClient)(nil)

func TestMockClient_ImportCertificate_NewCert(t *testing.T) {
	m := NewMockClient("arn:aws:acm:us-east-1:123456789012:certificate/mock-new")
	arn, err := m.ImportCertificate(context.Background(), "", []byte("cert"), []byte("key"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arn != "arn:aws:acm:us-east-1:123456789012:certificate/mock-new" {
		t.Errorf("unexpected ARN: %s", arn)
	}
	if len(m.ImportCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.ImportCalls))
	}
	if m.ImportCalls[0].ARN != "" {
		t.Errorf("expected empty ARN in call, got %q", m.ImportCalls[0].ARN)
	}
}

func TestMockClient_ImportCertificate_ExistingARN(t *testing.T) {
	m := NewMockClient("")
	existingARN := "arn:aws:acm:us-east-1:123:certificate/existing"
	arn, err := m.ImportCertificate(context.Background(), existingARN, []byte("cert"), []byte("key"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arn != existingARN {
		t.Errorf("expected %q, got %q", existingARN, arn)
	}
}

func TestMockClient_ImportCertificate_CustomFunc(t *testing.T) {
	m := NewMockClient("")
	m.ImportCertificateFunc = func(_ context.Context, _ string, _ []byte, _ []byte, _ []byte) (string, error) {
		return "", fmt.Errorf("custom error")
	}
	_, err := m.ImportCertificate(context.Background(), "", []byte("cert"), []byte("key"), nil)
	if err == nil || err.Error() != "custom error" {
		t.Errorf("expected custom error, got %v", err)
	}
}

func TestMockClient_AddTags(t *testing.T) {
	m := NewMockClient("")
	tags := map[string]string{"env": "test"}
	err := m.AddTags(context.Background(), "arn:aws:acm:us-east-1:123:certificate/abc", tags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.TagCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.TagCalls))
	}
	if m.TagCalls[0].Tags["env"] != "test" {
		t.Error("tag not recorded")
	}
}

func TestMockClient_AddTags_CustomFunc(t *testing.T) {
	m := NewMockClient("")
	m.AddTagsFunc = func(_ context.Context, _ string, _ map[string]string) error {
		return fmt.Errorf("tag error")
	}
	err := m.AddTags(context.Background(), "arn:aws:acm:us-east-1:123:certificate/abc", map[string]string{"k": "v"})
	if err == nil || err.Error() != "tag error" {
		t.Errorf("expected tag error, got %v", err)
	}
}

func TestMockClient_NoARNConfigured(t *testing.T) {
	m := NewMockClient("")
	_, err := m.ImportCertificate(context.Background(), "", []byte("cert"), []byte("key"), nil)
	if err == nil {
		t.Fatal("expected error when no ARN configured")
	}
}

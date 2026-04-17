//go:build e2e

package acm_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
)

func acmEndpoint() string {
	if ep := os.Getenv("ACM_ENDPOINT"); ep != "" {
		return ep
	}
	return "http://localhost:5000"
}

func TestACM_ImportReimportTag(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awsconfig.WithBaseEndpoint(acmEndpoint()),
	)
	if err != nil {
		t.Fatalf("failed to load AWS config: %v", err)
	}

	client := awsacm.NewFromConfig(cfg)
	certPEM, keyPEM := generateCert(t)

	// Import a new certificate.
	t.Log("importing a new certificate")
	out, err := client.ImportCertificate(ctx, &awsacm.ImportCertificateInput{
		Certificate: certPEM,
		PrivateKey:  keyPEM,
	})
	if err != nil {
		t.Fatalf("ImportCertificate failed: %v", err)
	}
	if out.CertificateArn == nil || *out.CertificateArn == "" {
		t.Fatal("expected non-empty ARN")
	}
	t.Logf("imported certificate: %s", *out.CertificateArn)

	// Re-import (update in place).
	t.Log("re-importing (updating) the certificate")
	out2, err := client.ImportCertificate(ctx, &awsacm.ImportCertificateInput{
		CertificateArn: out.CertificateArn,
		Certificate:    certPEM,
		PrivateKey:     keyPEM,
	})
	if err != nil {
		t.Fatalf("re-import failed: %v", err)
	}
	if *out2.CertificateArn != *out.CertificateArn {
		t.Errorf("expected same ARN %q, got %q", *out.CertificateArn, *out2.CertificateArn)
	}

	// Add tags.
	t.Log("adding tags")
	_, err = client.AddTagsToCertificate(ctx, &awsacm.AddTagsToCertificateInput{
		CertificateArn: out.CertificateArn,
		Tags: []acmtypes.Tag{
			{Key: aws.String("env"), Value: aws.String("e2e-test")},
		},
	})
	if err != nil {
		t.Fatalf("AddTagsToCertificate failed: %v", err)
	}

	t.Log("e2e happy path passed")
}

func generateCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "e2e-test.example.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return
}

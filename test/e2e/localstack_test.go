//go:build e2e

package e2e

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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// acmEndpoint returns the mock ACM endpoint URL.
// Supports both moto (default :5000) and LocalStack (:4566).
func acmEndpoint() string {
	if ep := os.Getenv("ACM_ENDPOINT"); ep != "" {
		return ep
	}
	return "http://localhost:5000"
}

var _ = Describe("LocalStack ACM Happy Path", func() {
	It("should import, re-import, and tag a certificate", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion("us-east-1"),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
			awsconfig.WithBaseEndpoint(acmEndpoint()),
		)
		Expect(err).NotTo(HaveOccurred())

		client := awsacm.NewFromConfig(cfg)

		certPEM, keyPEM := generateTestCertGinkgo()

		By("importing a new certificate")
		out, err := client.ImportCertificate(ctx, &awsacm.ImportCertificateInput{
			Certificate: certPEM,
			PrivateKey:  keyPEM,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.CertificateArn).NotTo(BeNil())
		GinkgoWriter.Printf("imported certificate: %s\n", *out.CertificateArn)

		By("re-importing (updating) the certificate")
		out2, err := client.ImportCertificate(ctx, &awsacm.ImportCertificateInput{
			CertificateArn: out.CertificateArn,
			Certificate:    certPEM,
			PrivateKey:     keyPEM,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(*out2.CertificateArn).To(Equal(*out.CertificateArn))

		By("adding tags")
		_, err = client.AddTagsToCertificate(ctx, &awsacm.AddTagsToCertificateInput{
			CertificateArn: out.CertificateArn,
			Tags: []acmtypes.Tag{
				{Key: aws.String("env"), Value: aws.String("e2e-test")},
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})
})

func generateTestCertGinkgo() (certPEM, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "e2e-test.example.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	Expect(err).NotTo(HaveOccurred())
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return
}

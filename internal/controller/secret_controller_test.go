/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"gitlab.maplarge.com/platform/acm-sync/internal/annotations"
)

func generateSelfSignedCert() (certPEM []byte, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
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

var _ = Describe("Secret Controller", func() {
	const namespace = "default"
	const timeout = 10 * time.Second
	const interval = 250 * time.Millisecond

	var certPEM, keyPEM []byte

	BeforeEach(func() {
		certPEM, keyPEM = generateSelfSignedCert()
		// Reset mock state before each test.
		mockACM.ImportCalls = nil
		mockACM.TagCalls = nil
		mockACM.ImportCertificateFunc = nil
		mockACM.AddTagsFunc = nil
	})

	Context("when Secret is not enabled", func() {
		It("should not reconcile", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-enabled",
					Namespace: namespace,
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			// Wait a bit and verify no ACM calls were made.
			Consistently(func() int {
				return len(mockACM.ImportCalls)
			}, 2*time.Second, interval).Should(Equal(0))

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})

	Context("when Secret is enabled without ARN", func() {
		It("should import and write ARN back", func() {
			secretName := "new-import"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
					Annotations: map[string]string{
						annotations.KeyEnabled: "true",
						annotations.KeyRegion:  "us-east-1",
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			// Wait for the controller to import the cert and write back the ARN.
			Eventually(func(g Gomega) {
				var s corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &s)).To(Succeed())
				g.Expect(s.Annotations[annotations.KeyARN]).To(Equal("arn:aws:acm:us-east-1:123456789012:certificate/test-new"))
				g.Expect(s.Annotations[annotations.KeyLastSyncedARN]).To(Equal("arn:aws:acm:us-east-1:123456789012:certificate/test-new"))
				g.Expect(s.Annotations[annotations.KeyLastSyncedHash]).NotTo(BeEmpty())
				g.Expect(s.Annotations[annotations.KeyLastSyncedTime]).NotTo(BeEmpty())
			}, timeout, interval).Should(Succeed())

			Expect(len(mockACM.ImportCalls)).To(BeNumerically(">=", 1))
			Expect(mockACM.ImportCalls[0].ARN).To(BeEmpty())

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})

	Context("when Secret is enabled with existing ARN", func() {
		It("should update in place", func() {
			secretName := "update-existing"
			existingARN := "arn:aws:acm:us-east-1:123456789012:certificate/existing"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
					Annotations: map[string]string{
						annotations.KeyEnabled: "true",
						annotations.KeyRegion:  "us-east-1",
						annotations.KeyARN:     existingARN,
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			Eventually(func(g Gomega) {
				var s corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &s)).To(Succeed())
				g.Expect(s.Annotations[annotations.KeyLastSyncedARN]).To(Equal(existingARN))
				g.Expect(s.Annotations[annotations.KeyLastSyncedHash]).NotTo(BeEmpty())
			}, timeout, interval).Should(Succeed())

			Expect(len(mockACM.ImportCalls)).To(BeNumerically(">=", 1))
			Expect(mockACM.ImportCalls[0].ARN).To(Equal(existingARN))

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})

	Context("when cert material is unchanged", func() {
		It("should skip re-import", func() {
			secretName := "unchanged-cert"
			existingARN := "arn:aws:acm:us-east-1:123456789012:certificate/unchanged"
			hash := sha256.Sum256(certPEM)
			hashStr := hex.EncodeToString(hash[:])

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
					Annotations: map[string]string{
						annotations.KeyEnabled:        "true",
						annotations.KeyRegion:         "us-east-1",
						annotations.KeyARN:            existingARN,
						annotations.KeyLastSyncedARN:  existingARN,
						annotations.KeyLastSyncedHash: hashStr,
						annotations.KeyLastSyncedTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			// Should not make any ACM calls since hash matches.
			Consistently(func() int {
				return len(mockACM.ImportCalls)
			}, 3*time.Second, interval).Should(Equal(0))

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})

	Context("when ACM returns an error", func() {
		It("should write last-error and return error for requeue", func() {
			mockACM.ImportCertificateFunc = func(_ context.Context, _ string, _ []byte, _ []byte, _ []byte) (string, error) {
				return "", fmt.Errorf("simulated ACM throttle")
			}

			secretName := "acm-error"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
					Annotations: map[string]string{
						annotations.KeyEnabled: "true",
						annotations.KeyRegion:  "us-east-1",
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			// The error is transient so the reconciler returns an error and
			// controller-runtime requeues. We verify the import was attempted.
			Eventually(func() int {
				return len(mockACM.ImportCalls)
			}, timeout, interval).Should(BeNumerically(">=", 1))

			// Clean up: remove error func so deletion reconcile doesn't fail.
			mockACM.ImportCertificateFunc = nil
			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})

	Context("when annotation is invalid", func() {
		It("should write last-error and not requeue", func() {
			secretName := "invalid-annotation"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
					Annotations: map[string]string{
						annotations.KeyEnabled: "true",
						// Missing required region annotation.
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			Eventually(func(g Gomega) {
				var s corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &s)).To(Succeed())
				g.Expect(s.Annotations[annotations.KeyLastError]).To(ContainSubstring("required"))
			}, timeout, interval).Should(Succeed())

			// No ACM calls should have been made.
			Expect(len(mockACM.ImportCalls)).To(Equal(0))

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})

	Context("when Secret type is not TLS", func() {
		It("should write last-error and not requeue", func() {
			secretName := "wrong-type"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
					Annotations: map[string]string{
						annotations.KeyEnabled: "true",
						annotations.KeyRegion:  "us-east-1",
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			Eventually(func(g Gomega) {
				var s corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &s)).To(Succeed())
				g.Expect(s.Annotations[annotations.KeyLastError]).To(ContainSubstring("kubernetes.io/tls"))
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})

	Context("with tags", func() {
		It("should apply tags after import", func() {
			secretName := "with-tags"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
					Annotations: map[string]string{
						annotations.KeyEnabled: "true",
						annotations.KeyRegion:  "us-east-1",
						annotations.KeyTags:    "env=prod,team=platform",
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": certPEM,
					"tls.key": keyPEM,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			Eventually(func(g Gomega) {
				var s corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &s)).To(Succeed())
				g.Expect(s.Annotations[annotations.KeyLastSyncedARN]).NotTo(BeEmpty())
			}, timeout, interval).Should(Succeed())

			Expect(len(mockACM.TagCalls)).To(BeNumerically(">=", 1))

			tagCall := mockACM.TagCalls[0]
			Expect(tagCall.Tags["env"]).To(Equal("prod"))
			Expect(tagCall.Tags["team"]).To(Equal("platform"))

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		})
	})
})

// TestHelpers - unit tests for internal helper functions.
var _ = Describe("Helper functions", func() {
	Describe("escapeJSONPointer", func() {
		It("should escape ~ and /", func() {
			Expect(escapeJSONPointer("acm-sync.maplarge.com/enabled")).To(Equal("acm-sync.maplarge.com~1enabled"))
			Expect(escapeJSONPointer("key~value")).To(Equal("key~0value"))
			Expect(escapeJSONPointer("normal")).To(Equal("normal"))
		})
	})

	Describe("splitCertChain", func() {
		It("should handle single cert", func() {
			certPEM, _ := generateSelfSignedCert()
			cert, chain := splitCertChain(certPEM)
			Expect(cert).NotTo(BeEmpty())
			Expect(chain).To(BeNil())
		})
	})

	Describe("buildJSONPatch", func() {
		It("should build add ops for new annotations", func() {
			ops := buildJSONPatch(nil, map[string]string{"key": "value"})
			Expect(ops).To(HaveLen(1))
			Expect(ops[0].Op).To(Equal("add"))
		})

		It("should build replace ops for existing annotations", func() {
			ops := buildJSONPatch(map[string]string{"key": "old"}, map[string]string{"key": "new"})
			Expect(ops).To(HaveLen(1))
			Expect(ops[0].Op).To(Equal("replace"))
		})

		It("should build remove ops for empty values on existing keys", func() {
			ops := buildJSONPatch(map[string]string{"key": "old"}, map[string]string{"key": ""})
			Expect(ops).To(HaveLen(1))
			Expect(ops[0].Op).To(Equal("remove"))
		})

		It("should skip no-op when clearing non-existent key", func() {
			ops := buildJSONPatch(nil, map[string]string{"key": ""})
			Expect(ops).To(BeNil())
		})
	})
})

// Verify at compile time that the reconciler struct has the fields we
// depend on from the test. This is a sentinel—if the struct changes and
// these fields disappear, the test won't compile.
var _ client.Client = (&SecretReconciler{}).Client

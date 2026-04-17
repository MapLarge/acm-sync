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
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"gitlab.maplarge.com/platform/acm-sync/internal/acm"
	"gitlab.maplarge.com/platform/acm-sync/internal/annotations"
	"gitlab.maplarge.com/platform/acm-sync/internal/metrics"
)

const requeueInterval = 1 * time.Hour

// SecretReconciler reconciles a Secret object.
type SecretReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      events.EventRecorder
	ClientFactory acm.ClientFactory
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("secret", req.Name, "namespace", req.Namespace)
	start := time.Now()

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	input, err := annotations.Parse(secret.Annotations)
	if errors.Is(err, annotations.ErrNotEnabled) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		r.recordPermanentError(ctx, &secret, fmt.Sprintf("invalid annotations: %v", err))
		metrics.ReconcileTotal.WithLabelValues("error").Inc()
		metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
		return ctrl.Result{}, nil
	}

	if secret.Type != corev1.SecretTypeTLS {
		r.recordPermanentError(ctx, &secret, fmt.Sprintf("secret type must be %s, got %s", corev1.SecretTypeTLS, secret.Type))
		metrics.ReconcileTotal.WithLabelValues("error").Inc()
		metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
		return ctrl.Result{}, nil
	}
	certData := secret.Data["tls.crt"]
	keyData := secret.Data["tls.key"]
	if len(certData) == 0 || len(keyData) == 0 {
		r.recordPermanentError(ctx, &secret, "secret is missing tls.crt or tls.key data")
		metrics.ReconcileTotal.WithLabelValues("error").Inc()
		metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
		return ctrl.Result{}, nil
	}

	if err := annotations.ValidateARNPartitionForRegions(input.ARN, input.Regions); err != nil {
		r.recordPermanentError(ctx, &secret, err.Error())
		metrics.ReconcileTotal.WithLabelValues("error").Inc()
		metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
		return ctrl.Result{}, nil
	}

	hash := sha256sum(certData)
	status := annotations.ParseStatus(secret.Annotations)

	if hash == status.LastSyncedHash && input.ARN != "" && input.ARN == status.LastSyncedARN {
		log.V(1).Info("cert material unchanged, skipping sync", "hash", hash)
		metrics.ReconcileTotal.WithLabelValues("skip").Inc()
		metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
		return ctrl.Result{RequeueAfter: requeueInterval}, nil
	}

	cert, chain := splitCertChain(certData)

	var resultARN string
	for _, region := range input.Regions {
		log := log.WithValues("region", region)

		acmClient, err := r.ClientFactory.ClientForRegion(ctx, region)
		if err != nil {
			log.Error(err, "failed to create ACM client")
			metrics.ReconcileTotal.WithLabelValues("error").Inc()
			metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
			return ctrl.Result{}, fmt.Errorf("creating ACM client for region %s: %w", region, err)
		}

		arn, err := acmClient.ImportCertificate(ctx, input.ARN, cert, keyData, chain)
		if err != nil {
			log.Error(err, "failed to import certificate", "arn", input.ARN)
			metrics.ReconcileTotal.WithLabelValues("error").Inc()
			metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
			return ctrl.Result{}, fmt.Errorf("importing certificate to region %s: %w", region, err)
		}
		log.Info("imported certificate", "arn", arn)
		resultARN = arn

		if len(input.Tags) > 0 {
			if err := acmClient.AddTags(ctx, arn, input.Tags); err != nil {
				log.Error(err, "failed to add tags", "arn", arn)
				metrics.ReconcileTotal.WithLabelValues("error").Inc()
				metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())
				return ctrl.Result{}, fmt.Errorf("adding tags in region %s: %w", region, err)
			}
		}
	}

	// Write ARN back to input annotation for new imports.
	if input.ARN == "" && resultARN != "" {
		if err := r.patchAnnotations(ctx, &secret, map[string]string{
			annotations.KeyARN: resultARN,
		}); err != nil {
			return ctrl.Result{}, fmt.Errorf("writing ARN annotation: %w", err)
		}
	}

	// Write status annotations.
	now := time.Now().UTC().Format(time.RFC3339)
	statusUpdate := annotations.Status{
		LastSyncedARN:  resultARN,
		LastSyncedTime: now,
		LastSyncedHash: hash,
	}
	if err := r.patchAnnotations(ctx, &secret, annotations.StatusAnnotations(statusUpdate)); err != nil {
		return ctrl.Result{}, fmt.Errorf("writing status annotations: %w", err)
	}

	// Update Prometheus metrics.
	metrics.LastSyncTimestamp.WithLabelValues(secret.Name, secret.Namespace, resultARN).SetToCurrentTime()
	if expiry := parseCertExpiry(certData); !expiry.IsZero() {
		metrics.CertificateExpiry.WithLabelValues(secret.Name, secret.Namespace, resultARN).Set(float64(expiry.Unix()))
	}
	metrics.ReconcileTotal.WithLabelValues("success").Inc()
	metrics.ReconcileDuration.WithLabelValues().Observe(time.Since(start).Seconds())

	r.Recorder.Eventf(&secret, nil, corev1.EventTypeNormal, "Synced", "ImportCertificate", "Certificate synced to ACM: %s", resultARN)
	log.Info("reconciliation complete", "arn", resultARN, "hash", hash)

	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

func (r *SecretReconciler) recordPermanentError(ctx context.Context, secret *corev1.Secret, msg string) {
	log := logf.FromContext(ctx).WithValues("secret", secret.Name, "namespace", secret.Namespace)
	log.Info("permanent error, not requeueing", "error", msg)
	r.Recorder.Eventf(secret, nil, corev1.EventTypeWarning, "SyncFailed", "Reconcile", "%s", msg)
	_ = r.patchAnnotations(ctx, secret, map[string]string{
		annotations.KeyLastError: msg,
	})
}

func (r *SecretReconciler) patchAnnotations(ctx context.Context, secret *corev1.Secret, annos map[string]string) error {
	ops := buildJSONPatch(secret.Annotations, annos)
	if ops == nil {
		return nil
	}
	raw, err := json.Marshal(ops)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}
	if err := r.Patch(ctx, secret, client.RawPatch(types.JSONPatchType, raw)); err != nil {
		return err
	}
	// Keep in-memory annotations consistent so subsequent patches in the same
	// reconciliation see the updated state.
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	for k, v := range annos {
		if v == "" {
			delete(secret.Annotations, k)
		} else {
			secret.Annotations[k] = v
		}
	}
	return nil
}

type jsonPatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}

func buildJSONPatch(existing map[string]string, updates map[string]string) []jsonPatchOp {
	var ops []jsonPatchOp
	for k, v := range updates {
		path := "/metadata/annotations/" + escapeJSONPointer(k)
		if _, exists := existing[k]; exists {
			if v == "" {
				ops = append(ops, jsonPatchOp{Op: "remove", Path: path})
			} else {
				ops = append(ops, jsonPatchOp{Op: "replace", Path: path, Value: v})
			}
		} else if v != "" {
			ops = append(ops, jsonPatchOp{Op: "add", Path: path, Value: v})
		}
	}
	if len(ops) == 0 {
		return nil
	}
	return ops
}

func escapeJSONPointer(s string) string {
	result := make([]byte, 0, len(s))
	for i := range len(s) {
		switch s[i] {
		case '~':
			result = append(result, '~', '0')
		case '/':
			result = append(result, '~', '1')
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func splitCertChain(pemData []byte) (cert []byte, chain []byte) {
	block, rest := pem.Decode(pemData)
	if block == nil {
		return pemData, nil
	}
	cert = pem.EncodeToMemory(block)
	var chainBlocks []byte
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		chainBlocks = append(chainBlocks, pem.EncodeToMemory(block)...)
	}
	return cert, chainBlocks
}

func parseCertExpiry(pemData []byte) time.Time {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return time.Time{}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}
	}
	return cert.NotAfter
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		WithEventFilter(enabledPredicate()).
		Named("secret").
		Complete(r)
}

// SetupReconciler creates and registers the reconciler with the manager.
func SetupReconciler(mgr ctrl.Manager, factory acm.ClientFactory) error {
	r := &SecretReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		Recorder:      mgr.GetEventRecorder("acm-sync"),
		ClientFactory: factory,
	}
	return r.SetupWithManager(mgr)
}

func enabledPredicate() predicate.Predicate {
	isEnabled := func(obj client.Object) bool {
		annos := obj.GetAnnotations()
		return annos != nil && annos[annotations.KeyEnabled] == "true"
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isEnabled(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isEnabled(e.ObjectNew)
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isEnabled(e.Object)
		},
	}
}

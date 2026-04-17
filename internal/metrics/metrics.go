package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "acm_sync_reconcile_total",
			Help: "Total number of reconciliations by result (success, error, skip).",
		},
		[]string{"result"},
	)

	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "acm_sync_reconcile_duration_seconds",
			Help:    "Duration of reconciliation in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{},
	)

	CertificateExpiry = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "acm_sync_certificate_expiry_timestamp_seconds",
			Help: "Unix timestamp of certificate NotAfter for the most recently synced cert.",
		},
		[]string{"secret", "namespace", "arn"},
	)

	LastSyncTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "acm_sync_last_sync_timestamp_seconds",
			Help: "Unix timestamp of the last successful sync.",
		},
		[]string{"secret", "namespace", "arn"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ReconcileTotal,
		ReconcileDuration,
		CertificateExpiry,
		LastSyncTimestamp,
	)
}

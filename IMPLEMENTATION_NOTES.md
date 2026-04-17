# Implementation Notes

## What was built

Complete v1 of acm-sync as specified in CLAUDE.md "First Tasks":

1. **`internal/annotations/`** — Pure Go package for parsing and validating input annotations (`enabled`, `region`, `arn`, `tags`) and status annotations (`last-synced-arn`, `last-synced-time`, `last-synced-hash`, `last-error`). Includes `ErrNotEnabled` sentinel, ARN partition vs region cross-validation, `aws:` tag key rejection, and ACM tag length limits (128/256). 98.8% test coverage.

2. **`internal/acm/`** — `Client` interface with `ImportCertificate` and `AddTags` methods. `RealClient` wraps `aws-sdk-go-v2` ACM SDK. `MockClient` with call recording for tests. `ClientFactory` interface with `SDKClientFactory` (partition-aware, FIPS-configurable) and `MockClientFactory`. 80.9% test coverage.

3. **`internal/controller/`** — `SecretReconciler` watching core `v1.Secret` cluster-wide. Predicate filters by `acm-sync.maplarge.com/enabled=true` at the work queue level. SHA256 change detection on `tls.crt`. Import-or-update based on ARN annotation presence. Writes ARN back for new imports. Status annotations via JSON Patch. Events via new `events.EventRecorder` API. Transient errors requeue; permanent errors record event + `last-error` + return nil. 80.3% test coverage.

4. **`internal/metrics/`** — Prometheus metrics registered via controller-runtime's built-in registry: `acm_sync_reconcile_total{result}`, `acm_sync_reconcile_duration_seconds`, `acm_sync_certificate_expiry_timestamp_seconds{secret,namespace,arn}`, `acm_sync_last_sync_timestamp_seconds{secret,namespace,arn}`.

5. **`cmd/main.go`** — Wires up manager, ACM client factory, reconciler. Flags: `--use-fips-endpoint` (also via `AWS_USE_FIPS_ENDPOINT` env), `--metrics-bind-address`, `--health-probe-bind-address`, `--leader-elect`, `--metrics-secure`, `--zap-*`. No webhook server (not needed).

6. **Helm chart** at `charts/acm-sync/` — Deployment, ServiceAccount (IRSA annotations configurable), ClusterRole (least privilege: get/list/watch/patch secrets, create/patch events), ClusterRoleBinding, Service for metrics, optional ServiceMonitor. Values support FIPS flag, resource limits, pod security context (non-root, read-only rootfs, drop ALL), nodeSelector/tolerations/affinity. CI fixtures for commercial and GovCloud.

7. **Dockerfile** — Multi-stage build, `gcr.io/distroless/static:nonroot` base, runs as UID 65532, supports `linux/amd64` and `linux/arm64` via `BUILDPLATFORM`/`TARGETARCH`.

8. **E2E stub** — `test/e2e/docker-compose.yaml` with LocalStack ACM service, plus a happy-path test (import, re-import, tag) behind the `e2e` build tag.

9. **RBAC** — kubebuilder markers generate `config/rbac/role.yaml` with exactly: get/list/watch/patch on secrets, create/patch on events.

## Non-obvious interpretations

- **Multi-region ARN writeback**: Only one ARN annotation exists, so multi-region imports write back the last region's ARN. See DECISIONS.md #2.
- **`parseCSV` used for both regions and tags**: Reuses the same CSV parser since both annotations are comma-separated. Tags additionally split on `=`.
- **Certificate expiry metric**: Parsed from the first PEM block in `tls.crt` using `x509.ParseCertificate`. If parsing fails (e.g., non-standard PEM), the metric is silently not set rather than failing the reconciliation.

## Top 3 things for reviewer attention

1. **Multi-region ARN semantics** — The current design stores a single ARN but supports multi-region fan-out. Each region gets a different ACM ARN, but only one is written back. If multi-region is actually used, customers will need to track per-region ARNs externally. Consider whether this needs a richer annotation scheme before shipping.

2. **JSON Patch annotation updates** — The patch logic handles add/replace/remove based on whether the annotation key already exists and whether the new value is empty. The `patchAnnotations` method also updates the in-memory Secret object so subsequent patches in the same reconciliation see consistent state. Edge case: if the API server rejects the patch (conflict), the in-memory state diverges. The requeue will fix this, but verify the behavior under high-contention scenarios.

3. **Predicate filtering** — The `enabledPredicate` filters at the work queue level, so Secrets without `acm-sync.maplarge.com/enabled=true` never enter the reconcile loop. This keeps the queue clean but means if someone removes the annotation from a Secret that was previously synced, the controller won't see the change and won't clean up `last-error` or other status annotations. This is intentional for v1 (no finalizer/cleanup scope), but worth noting.

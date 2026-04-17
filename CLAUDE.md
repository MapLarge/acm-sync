# acm-sync

A Kubernetes controller that syncs TLS certificates from Kubernetes Secrets (typically managed by cert-manager) into AWS Certificate Manager (ACM). Eliminates manual cert rotation in ACM when cert-manager already pulls fresh certs from the issuer.

## Purpose

cert-manager handles the hard part — issuing and renewing certs from ACME/Vault/etc. into Kubernetes Secrets. But downstream AWS resources (ALB listeners, CloudFront distributions, API Gateway custom domains) reference certificates by ACM ARN, not by Kubernetes Secret. `acm-sync` closes that gap: when a Secret rotates, the controller imports the new cert material into ACM using the existing ARN, so downstream AWS resources pick up the new cert with no infrastructure changes.

## Tech Stack

- **Language**: Go 1.23+
- **Framework**: kubebuilder v4 + controller-runtime
- **AWS SDK**: aws-sdk-go-v2 (partition-aware config loading required)
- **Testing**: envtest for controller logic, mocked ACM client interface for AWS interactions
- **Logging**: structured logging via `logr` (controller-runtime default)
- **Metrics**: Prometheus via controller-runtime's built-in registry
- **Packaging**: Helm chart in `charts/acm-sync/` for deployment; kubebuilder manifests retained for reference

## Design Overview

### Discovery

The controller watches core `Secret` resources cluster-wide and reconciles only those with:

```
acm-sync.maplarge.com/enabled: "true"
```

Supported annotations on the Secret:

| Annotation | Required | Description |
|---|---|---|
| `acm-sync.maplarge.com/enabled` | yes | Must be `"true"` to opt in |
| `acm-sync.maplarge.com/region` | yes | Target AWS region (comma-separated for multi-region fan-out) |
| `acm-sync.maplarge.com/arn` | no | Existing ACM ARN. If absent, controller imports a new cert and writes the ARN back |
| `acm-sync.maplarge.com/tags` | no | Comma-separated `key=value` pairs applied to the ACM cert |

The controller writes status back to the Secret via annotations (not a separate CRD — keeps things simple for v1):

| Annotation | Description |
|---|---|
| `acm-sync.maplarge.com/last-synced-arn` | The ARN written on the most recent successful sync |
| `acm-sync.maplarge.com/last-synced-time` | RFC3339 timestamp of last successful sync |
| `acm-sync.maplarge.com/last-synced-hash` | SHA256 of `tls.crt` to detect material changes |
| `acm-sync.maplarge.com/last-error` | Most recent error message; cleared on success |

### Reconcile Logic

1. Fetch Secret; verify `kubernetes.io/tls` type and presence of `tls.crt` + `tls.key`.
2. Compute SHA256 of `tls.crt`. If equal to `last-synced-hash` and ARN annotation matches `last-synced-arn`, requeue with long interval and exit.
3. For each region in the `region` annotation:
   - If `arn` annotation present: call `acm:ImportCertificate` with that ARN (in-place update).
   - If absent: call `acm:ImportCertificate` without ARN, receive new ARN, patch Secret annotation with returned ARN.
4. Apply tags via `acm:AddTagsToCertificate`.
5. Update status annotations on the Secret.
6. Emit metrics and events.

### Partition Awareness (HARD REQUIREMENT)

This controller must run in both AWS commercial (`aws`) and AWS GovCloud (`aws-us-gov`) partitions. Design constraints:

- **Never hardcode partition or endpoints.** Use `aws-sdk-go-v2` default config loading, which resolves partition from region.
- **FIPS support** — expose a `--use-fips-endpoint` flag (and `AWS_USE_FIPS_ENDPOINT` env var). Required for many DoD customers.
- **IRSA per partition** — the Helm chart must support configuring the service account IAM role ARN; document both commercial and GovCloud OIDC trust policy examples in `charts/acm-sync/README.md`.
- **No cross-partition calls.** One deployment per partition. Do not attempt to bridge.
- **Test matrix** should include at minimum: `us-east-1`, `us-west-2`, `us-gov-west-1`.

### RBAC Principle

Least privilege. The controller's ClusterRole should have:

- `get`, `list`, `watch`, `patch` on `secrets` (patch is needed to write status annotations)
- `create`, `patch` on `events` (for `EventRecorder`)

No access to other resource types. Do not request cluster-admin or anything broader during development just to move faster — it will leak into the Helm chart.

### Required ACM IAM Permissions

Document these in the Helm chart README for both partitions:

- `acm:ImportCertificate`
- `acm:AddTagsToCertificate`
- `acm:ListTagsForCertificate`
- `acm:DescribeCertificate` (for validation on startup and drift detection)

## Repository Layout

```
acm-sync/
├── cmd/
│   └── main.go
├── internal/
│   ├── controller/           # Secret reconciler
│   ├── acm/                  # AWS ACM client wrapper (partition-aware, interface for mocking)
│   └── annotations/          # annotation parsing + validation
├── config/                   # kubebuilder manifests (retained for RBAC generation)
├── charts/
│   └── acm-sync/             # Helm chart — primary deployment mechanism
├── test/
│   └── e2e/
├── CLAUDE.md
├── README.md
└── Makefile
```

## Conventions

### Code style
- Standard Go formatting (`gofmt`, `goimports`).
- `golangci-lint` with the kubebuilder-default config as a starting point.
- Interfaces for any AWS client usage — no direct SDK calls inside reconciler logic. This keeps tests fast and deterministic.

### Logging
- Structured logging via `logr`. Include `secret`, `namespace`, `region`, and `arn` as fields on every log line in the reconciler.
- No `fmt.Println` or `log.Printf` in production code paths.

### Error handling
- Transient errors (AWS throttling, network) → return error from `Reconcile` to let controller-runtime requeue with backoff.
- Permanent errors (malformed cert, missing required annotation) → record event, update `last-error` annotation, return `nil` to avoid hot-looping.

### Testing
- Unit tests: annotation parser, ACM client wrapper (with mock), reconciler (with envtest + mock ACM).
- E2E tests: ideally against LocalStack for CI; optional real-account smoke test target in Makefile.
- Aim for >80% coverage on `internal/` packages.

### Commits
- Conventional commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`).
- One logical change per commit.

## Future Considerations

Not in scope for v1 but worth keeping the design open to:

- **Read-only / Terraform-managed mode** — a flag that makes the controller refuse to create new ACM certs, only update existing ARNs. Some customers prefer ACM lifecycle stay in Terraform for auditability.
- **Certificate deletion** — when a Secret is deleted or the annotation removed, should the ACM cert be deleted? Dangerous (downstream resources break). Likely needs a finalizer + explicit opt-in annotation.
- **Drift detection** — periodically compare Secret material to ACM and re-sync if they've diverged (e.g., someone manually updated ACM).
- **Metrics for cert expiry** — expose a Prometheus gauge for cert NotAfter so alerts can fire independent of sync success.
- **Multi-cluster deduplication** — if two clusters sync the same ARN, they'll fight. Consider a leader annotation or cluster-ID tag.

## First Tasks (suggested sequence)

1. `kubebuilder init` + scaffold Secret reconciler (no CRD — watch core Secrets).
2. `internal/annotations/` package: parse + validate, with table-driven tests.
3. `internal/acm/` package: partition-aware client, interface + real implementation + mock.
4. Reconcile logic: import-or-update based on ARN annotation presence; hash-based change detection.
5. Status annotation writeback.
6. Prometheus metrics: sync success/failure counters, last-sync-timestamp gauge, cert-expiry gauge.
7. Helm chart with commercial + GovCloud value examples, FIPS flag, IRSA annotations.
8. E2E test with LocalStack.

## Non-Goals

- Issuing certificates (that's cert-manager's job).
- Managing ALB/CloudFront/API Gateway resources (that's Terraform's job).
- Cross-partition sync (architecturally impossible and out of scope).
- Serving as a general-purpose AWS secret sync tool (scope is ACM only; use External Secrets Operator for broader needs).

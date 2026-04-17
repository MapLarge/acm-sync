# acm-sync

A Kubernetes controller that syncs TLS certificates from Kubernetes Secrets into AWS Certificate Manager (ACM).

## What it does

cert-manager handles issuance and renewal of TLS certificates into Kubernetes Secrets. But AWS load balancers, CloudFront distributions, and API Gateway custom domains reference certificates by ACM ARN — not by Kubernetes Secret. Without automation, every cert renewal requires a manual ACM import or a Terraform apply.

`acm-sync` watches annotated Secrets and keeps ACM in sync. When cert-manager rotates a cert, the controller imports the new material into the existing ACM ARN, so downstream AWS resources pick up the new cert with no infrastructure changes.

## How it works

The controller watches `Secret` resources cluster-wide. Secrets opt in via an annotation:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: api-tls
  namespace: production
  annotations:
    acm-sync.maplarge.com/enabled: "true"
    acm-sync.maplarge.com/region: "us-east-1"
    acm-sync.maplarge.com/arn: "arn:aws:acm:us-east-1:123456789012:certificate/abcd-1234"
    acm-sync.maplarge.com/tags: "env=prod,team=platform"
type: kubernetes.io/tls
data:
  tls.crt: ...
  tls.key: ...
```

When the Secret changes, the controller:

1. Detects material change via SHA256 of `tls.crt`.
2. Calls `acm:ImportCertificate` against the ARN in the annotation (in-place update).
3. Applies tags and updates status annotations on the Secret.

If the `arn` annotation is absent, the controller imports a new certificate and writes the returned ARN back to the Secret. This keeps bootstrap simple — create the Secret, and the ARN lands on it automatically.

## Annotations

### Input (set by you or cert-manager)

| Annotation | Required | Description |
|---|---|---|
| `acm-sync.maplarge.com/enabled` | yes | Must be `"true"` to opt in |
| `acm-sync.maplarge.com/region` | yes | Target AWS region (comma-separated for multi-region) |
| `acm-sync.maplarge.com/arn` | no | Existing ACM ARN; if absent, a new cert is imported |
| `acm-sync.maplarge.com/tags` | no | Comma-separated `key=value` pairs |

### Status (written by the controller)

| Annotation | Description |
|---|---|
| `acm-sync.maplarge.com/last-synced-arn` | ARN written on the most recent successful sync |
| `acm-sync.maplarge.com/last-synced-time` | RFC3339 timestamp of last successful sync |
| `acm-sync.maplarge.com/last-synced-hash` | SHA256 of `tls.crt` from last sync |
| `acm-sync.maplarge.com/last-error` | Most recent error; cleared on success |

## Deployment

### Prerequisites

- Kubernetes 1.28+ (EKS commercial or GovCloud)
- cert-manager installed (or any other source writing `kubernetes.io/tls` Secrets)
- An IAM role for the controller with IRSA configured

### Installation

```bash
helm install acm-sync ./charts/acm-sync \
  --namespace acm-sync \
  --create-namespace \
  --values values.example.yaml
```

See `charts/acm-sync/README.md` for partition-specific values (commercial vs. GovCloud) and FIPS configuration.

### IAM Permissions

The controller's IAM role requires:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "acm:ImportCertificate",
        "acm:AddTagsToCertificate",
        "acm:ListTagsForCertificate",
        "acm:DescribeCertificate"
      ],
      "Resource": "*"
    }
  ]
}
```

`Resource: "*"` is required for `ImportCertificate` without a pre-existing ARN. Once all certs have stable ARNs, you can scope this down to specific ARN prefixes.

## AWS Partition Support

`acm-sync` runs in both AWS commercial and AWS GovCloud. Partition and endpoints are resolved automatically from the configured region — no partition-specific builds.

| Partition | Regions tested |
|---|---|
| `aws` | `us-east-1`, `us-west-2` |
| `aws-us-gov` | `us-gov-west-1` |

For FIPS-required deployments, set `useFipsEndpoint: true` in Helm values.

## Observability

### Metrics

Prometheus metrics exposed on `:8080/metrics`:

- `acm_sync_reconcile_total{result="success|error"}`
- `acm_sync_reconcile_duration_seconds`
- `acm_sync_certificate_expiry_timestamp_seconds{secret,namespace,arn}`
- `acm_sync_last_sync_timestamp_seconds{secret,namespace,arn}`

### Events

The controller emits Kubernetes events on each sync attempt. Use `kubectl describe secret <name>` to see recent activity.

### Logs

Structured JSON logs with `secret`, `namespace`, `region`, and `arn` fields on every reconcile.

## Local development

See [CLAUDE.md](./CLAUDE.md) for project conventions, then:

```bash
make help
```

Common targets:

```bash
make build          # Compile binary
make test           # Unit tests
make lint           # golangci-lint
make run            # Run controller locally against current kubecontext
make e2e            # E2E tests against LocalStack
```

## Non-Goals

- Issuing certificates (use cert-manager).
- Managing ALB / CloudFront / API Gateway resources (use Terraform).
- Cross-partition sync (architecturally impossible).
- General-purpose AWS secret sync (use External Secrets Operator).

## License

TBD — internal MapLarge project.

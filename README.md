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

- Kubernetes 1.28+
- cert-manager installed (or any other source writing `kubernetes.io/tls` Secrets)
- AWS credentials available to the controller via any method supported by the SDK v2 default credential chain:
  - **EKS Pod Identity** (recommended for EKS)
  - **IRSA** (IAM Roles for Service Accounts, for EKS with OIDC)
  - **Environment variables** (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`, for non-EKS clusters)
  - **EC2 instance metadata** (IMDS, for self-managed clusters on EC2)
  - **Shared credentials file** (for local development)

### Installation

The Helm chart is published as an OCI artifact to GitHub Container Registry:

```bash
helm install acm-sync oci://ghcr.io/maplarge/charts/acm-sync \
  --namespace acm-sync \
  --create-namespace
```

To install a specific version:

```bash
helm install acm-sync oci://ghcr.io/maplarge/charts/acm-sync --version 1.2.3 \
  --namespace acm-sync \
  --create-namespace
```

See `charts/acm-sync/README.md` for the full values reference, partition-specific examples (commercial vs. GovCloud), FIPS configuration, and authentication setup for EKS Pod Identity, IRSA, and environment variables.

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

### AWS Authentication

The controller uses the AWS SDK v2 default credential chain, which tries the following sources in order:

1. **Environment variables** (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optionally `AWS_SESSION_TOKEN`)
2. **Shared credentials file** (`~/.aws/credentials`)
3. **EKS Pod Identity** (via the Pod Identity Agent injecting container credentials)
4. **IRSA** (via `AWS_WEB_IDENTITY_TOKEN_FILE` and `AWS_ROLE_ARN` projected by EKS)
5. **EC2 instance metadata** (IMDS)

No code or configuration changes are needed to switch between methods — the SDK resolves credentials automatically. For production EKS deployments, Pod Identity or IRSA are recommended. Environment variables can be useful for local development or non-EKS clusters.

#### Environment variables

Set credentials directly on the controller pod (e.g., via a Kubernetes Secret mounted as env vars):

```bash
helm install acm-sync ./charts/acm-sync \
  --namespace acm-sync --create-namespace \
  --set-json 'extraEnv=[{"name":"AWS_ACCESS_KEY_ID","valueFrom":{"secretKeyRef":{"name":"aws-creds","key":"access-key-id"}}},{"name":"AWS_SECRET_ACCESS_KEY","valueFrom":{"secretKeyRef":{"name":"aws-creds","key":"secret-access-key"}}}]'
```

This is the simplest approach for non-EKS clusters or local testing, but requires managing credential rotation yourself.

#### EKS Pod Identity (recommended)

EKS Pod Identity is the newer, simpler approach. It doesn't require an OIDC provider or ServiceAccount annotations — you create an association between the IAM role and the Kubernetes ServiceAccount directly via the EKS API.

1. Create the IAM role with the ACM permissions above and a trust policy for the EKS Pod Identity service principal:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "pods.eks.amazonaws.com"
      },
      "Action": [
        "sts:AssumeRole",
        "sts:TagSession"
      ]
    }
  ]
}
```

2. Create the Pod Identity association:

```bash
aws eks create-pod-identity-association \
  --cluster-name MY_CLUSTER \
  --namespace acm-sync \
  --service-account acm-sync \
  --role-arn arn:aws:iam::123456789012:role/acm-sync
```

3. Install the chart with no ServiceAccount annotations needed:

```bash
helm install acm-sync ./charts/acm-sync \
  --namespace acm-sync --create-namespace
```

#### IRSA (IAM Roles for Service Accounts)

For clusters without Pod Identity support, or when using self-managed OIDC providers:

```bash
helm install acm-sync ./charts/acm-sync \
  --namespace acm-sync --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/acm-sync
```

See `charts/acm-sync/README.md` for OIDC trust policy examples for both commercial and GovCloud.

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

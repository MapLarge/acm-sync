# acm-sync Helm Chart

Deploys the acm-sync controller, which syncs TLS certificates from Kubernetes Secrets into AWS Certificate Manager (ACM).

## Prerequisites

- Kubernetes 1.28+
- Helm 3.x
- An IAM identity with the required ACM permissions (see below), provided via any method the AWS SDK v2 default credential chain supports:
  - **EKS Pod Identity** (recommended for EKS) — requires the Pod Identity Agent add-on
  - **IRSA** (for EKS with OIDC) — requires an OIDC provider configured on the cluster
  - **Environment variables** (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`) — for non-EKS clusters; pass via `extraEnv`
  - **EC2 instance metadata** (IMDS) — for self-managed clusters running on EC2
  - **Shared credentials file** — primarily for local development

## Required IAM Permissions

The controller needs the following ACM permissions:

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

## AWS Authentication

The controller uses the AWS SDK v2 default credential chain, which tries these sources in order:

1. **Environment variables** (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, optionally `AWS_SESSION_TOKEN`)
2. **Shared credentials file** (`~/.aws/credentials`)
3. **EKS Pod Identity** (via the Pod Identity Agent)
4. **IRSA** (via `AWS_WEB_IDENTITY_TOKEN_FILE` and `AWS_ROLE_ARN`)
5. **EC2 instance metadata** (IMDS)

No code changes are needed to switch between methods. For production EKS deployments, Pod Identity or IRSA are recommended. Environment variables are useful for non-EKS clusters or local development.

### Environment variables

For non-EKS clusters or development, you can pass credentials via environment variables by mounting a Kubernetes Secret:

```yaml
# In your values override:
extraEnv:
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef:
        name: aws-creds
        key: access-key-id
  - name: AWS_SECRET_ACCESS_KEY
    valueFrom:
      secretKeyRef:
        name: aws-creds
        key: secret-access-key
```

You are responsible for rotating these credentials. Prefer Pod Identity or IRSA when running on EKS.

### Option 1: EKS Pod Identity (recommended)

Pod Identity is simpler to set up — no OIDC provider or ServiceAccount annotations required.

1. Ensure the [EKS Pod Identity Agent add-on](https://docs.aws.amazon.com/eks/latest/userguide/pod-id-agent-setup.html) is installed on your cluster.

2. Create an IAM role with the ACM permissions above and this trust policy:

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

For GovCloud, the same trust policy applies — the `pods.eks.amazonaws.com` service principal is partition-aware.

3. Create the association:

```bash
# Commercial
aws eks create-pod-identity-association \
  --cluster-name MY_CLUSTER \
  --namespace acm-sync \
  --service-account acm-sync \
  --role-arn arn:aws:iam::123456789012:role/acm-sync

# GovCloud
aws eks create-pod-identity-association \
  --cluster-name MY_CLUSTER \
  --namespace acm-sync \
  --service-account acm-sync \
  --role-arn arn:aws-us-gov:iam::123456789012:role/acm-sync
```

4. Install with no ServiceAccount annotations:

```bash
helm install acm-sync ./charts/acm-sync \
  --namespace acm-sync --create-namespace
```

### Option 2: IRSA (IAM Roles for Service Accounts)

Use IRSA when Pod Identity is not available (e.g., self-managed clusters with an OIDC provider, or older EKS platform versions).

#### Commercial OIDC Trust Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::ACCOUNT_ID:oidc-provider/oidc.eks.REGION.amazonaws.com/id/CLUSTER_ID"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "oidc.eks.REGION.amazonaws.com/id/CLUSTER_ID:sub": "system:serviceaccount:NAMESPACE:acm-sync",
          "oidc.eks.REGION.amazonaws.com/id/CLUSTER_ID:aud": "sts.amazonaws.com"
        }
      }
    }
  ]
}
```

#### GovCloud OIDC Trust Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws-us-gov:iam::ACCOUNT_ID:oidc-provider/oidc.eks.REGION.amazonaws.com/id/CLUSTER_ID"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "oidc.eks.REGION.amazonaws.com/id/CLUSTER_ID:sub": "system:serviceaccount:NAMESPACE:acm-sync",
          "oidc.eks.REGION.amazonaws.com/id/CLUSTER_ID:aud": "sts.amazonaws.com"
        }
      }
    }
  ]
}
```

## Installation

The chart is published as an OCI artifact to GitHub Container Registry.

### From the OCI registry

```bash
# Install the latest version
helm install acm-sync oci://ghcr.io/maplarge/charts/acm-sync \
  --namespace acm-sync --create-namespace

# Install a specific version
helm install acm-sync oci://ghcr.io/maplarge/charts/acm-sync --version 1.2.3 \
  --namespace acm-sync --create-namespace

# Upgrade an existing release
helm upgrade acm-sync oci://ghcr.io/maplarge/charts/acm-sync --version 1.2.3 \
  --namespace acm-sync

# View available versions
helm show all oci://ghcr.io/maplarge/charts/acm-sync
```

OCI-based Helm charts do not require `helm repo add` — you reference the full OCI URL directly.

If your GHCR packages are private, authenticate first:

```bash
echo "$GITHUB_TOKEN" | helm registry login ghcr.io -u USERNAME --password-stdin
```

### From a local checkout

```bash
helm install acm-sync ./charts/acm-sync \
  --namespace acm-sync --create-namespace
```

### Authentication examples

#### With EKS Pod Identity

```bash
helm install acm-sync oci://ghcr.io/maplarge/charts/acm-sync \
  --namespace acm-sync --create-namespace
```

No ServiceAccount annotations are needed — the Pod Identity Agent injects credentials automatically based on the association created above.

#### With IRSA (Commercial)

```bash
helm install acm-sync oci://ghcr.io/maplarge/charts/acm-sync \
  --namespace acm-sync --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/acm-sync
```

#### With IRSA (GovCloud + FIPS)

```bash
helm install acm-sync oci://ghcr.io/maplarge/charts/acm-sync \
  --namespace acm-sync --create-namespace \
  --set controller.useFIPSEndpoint=true \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws-us-gov:iam::123456789012:role/acm-sync
```

## Values

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `ghcr.io/maplarge/acm-sync` | Container image repository |
| `image.tag` | `""` (appVersion) | Container image tag |
| `serviceAccount.create` | `true` | Create a ServiceAccount |
| `serviceAccount.annotations` | `{}` | Annotations for the ServiceAccount (set IRSA role ARN here; not needed for Pod Identity) |
| `controller.useFIPSEndpoint` | `false` | Use FIPS-compliant AWS endpoints |
| `controller.leaderElect` | `true` | Enable leader election |
| `controller.metricsBindAddress` | `:8080` | Metrics endpoint bind address |
| `controller.healthProbeBindAddress` | `:8081` | Health probe bind address |
| `controller.metricsSecure` | `true` | Serve metrics over HTTPS |
| `resources` | See values.yaml | CPU/memory requests and limits |
| `podSecurityContext` | non-root, RuntimeDefault seccomp | Pod-level security context |
| `securityContext` | read-only root fs, drop ALL caps | Container-level security context |
| `nodeSelector` | `{}` | Node selector |
| `tolerations` | `[]` | Tolerations |
| `affinity` | `{}` | Affinity rules |
| `podLabels` | `{}` | Extra labels added to the pod template |
| `podAnnotations` | `{}` | Extra annotations added to the pod template |
| `extraEnv` | `[]` | Extra environment variables for the controller (e.g., AWS credentials for non-EKS clusters) |
| `extraVolumeMounts` | `[]` | Extra volume mounts for the controller container |
| `extraVolumes` | `[]` | Extra volumes for the pod |
| `serviceMonitor.enabled` | `false` | Create a Prometheus ServiceMonitor |
| `serviceMonitor.interval` | `30s` | Scrape interval |
| `grafanaDashboard.enabled` | `false` | Create a ConfigMap with the Grafana dashboard JSON |
| `grafanaDashboard.namespace` | `""` (release namespace) | Namespace for the dashboard ConfigMap |
| `grafanaDashboard.labels` | `grafana_dashboard: "1"` | Labels for Grafana sidecar discovery |

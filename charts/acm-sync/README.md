# acm-sync Helm Chart

Deploys the acm-sync controller, which syncs TLS certificates from Kubernetes Secrets into AWS Certificate Manager (ACM).

## Prerequisites

- Kubernetes 1.28+
- Helm 3.x
- An IAM role with the required ACM permissions (see below)
- IRSA (IAM Roles for Service Accounts) configured on the EKS cluster

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

### Commercial OIDC Trust Policy

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

### GovCloud OIDC Trust Policy

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

### Commercial

```bash
helm install acm-sync ./charts/acm-sync \
  --namespace acm-sync --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/acm-sync
```

### GovCloud with FIPS

```bash
helm install acm-sync ./charts/acm-sync \
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
| `serviceAccount.annotations` | `{}` | Annotations for the ServiceAccount (IRSA role ARN) |
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
| `serviceMonitor.enabled` | `false` | Create a Prometheus ServiceMonitor |
| `serviceMonitor.interval` | `30s` | Scrape interval |

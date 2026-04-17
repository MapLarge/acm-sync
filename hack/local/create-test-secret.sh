#!/usr/bin/env bash
#
# Creates a TLS secret with a self-signed cert, annotated for acm-sync.
# The controller should pick it up and attempt an ACM import against LocalStack.
#
# Usage:
#   ./hack/local/create-test-secret.sh [secret-name] [namespace]

set -euo pipefail

SECRET_NAME="${1:-acm-sync-test}"
NAMESPACE="${2:-default}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "==> Generating self-signed certificate"
openssl req -x509 -newkey rsa:2048 \
    -keyout "${TMPDIR}/tls.key" -out "${TMPDIR}/tls.crt" \
    -days 90 -nodes -subj "/CN=test.example.com" 2>/dev/null

echo "==> Creating TLS secret '${SECRET_NAME}' in namespace '${NAMESPACE}'"
kubectl create secret tls "$SECRET_NAME" \
    --cert="${TMPDIR}/tls.crt" \
    --key="${TMPDIR}/tls.key" \
    --namespace "$NAMESPACE" \
    --dry-run=client -o yaml | kubectl apply -f -

echo "==> Annotating secret for acm-sync"
kubectl annotate secret "$SECRET_NAME" \
    --namespace "$NAMESPACE" \
    --overwrite \
    acm-sync.maplarge.com/enabled=true \
    acm-sync.maplarge.com/region=us-east-1 \
    acm-sync.maplarge.com/tags="env=local-dev,managed-by=acm-sync"

echo "==> Done. Watch the controller:"
echo "    kubectl logs -f -n acm-sync deployment/acm-sync"
echo ""
echo "    Check status annotations:"
echo "    kubectl get secret ${SECRET_NAME} -n ${NAMESPACE} -o jsonpath='{.metadata.annotations}' | jq ."

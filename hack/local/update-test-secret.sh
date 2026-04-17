#!/usr/bin/env bash
#
# Regenerates the TLS certificate material in an existing acm-sync-managed
# secret. The controller should detect the changed tls.crt hash and re-import
# to the same ACM ARN (in-place update).
#
# Usage:
#   ./hack/local/update-test-secret.sh [secret-name] [namespace]

set -euo pipefail

SECRET_NAME="${1:-acm-sync-test}"
NAMESPACE="${2:-default}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

# Verify the secret exists and has an ARN (meaning the first sync completed).
CURRENT_ARN="$(kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" \
    -o jsonpath='{.metadata.annotations.acm-sync\.maplarge\.com/arn}' 2>/dev/null || true)"

if [[ -z "$CURRENT_ARN" ]]; then
    echo "ERROR: Secret '${SECRET_NAME}' in namespace '${NAMESPACE}' does not have an acm-sync ARN annotation." >&2
    echo "Run ./hack/local/create-test-secret.sh first and wait for the initial sync to complete." >&2
    exit 1
fi

CURRENT_HASH="$(kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" \
    -o jsonpath='{.metadata.annotations.acm-sync\.maplarge\.com/last-synced-hash}' 2>/dev/null || true)"

echo "==> Current state:"
echo "    ARN:  $CURRENT_ARN"
echo "    Hash: $CURRENT_HASH"

echo "==> Generating new certificate (same CN, new key material)"
openssl req -x509 -newkey rsa:2048 \
    -keyout "${TMPDIR}/tls.key" -out "${TMPDIR}/tls.crt" \
    -days 90 -nodes -subj "/CN=test.example.com" 2>/dev/null

echo "==> Patching secret with new cert material"
kubectl create secret tls "$SECRET_NAME" \
    --cert="${TMPDIR}/tls.crt" \
    --key="${TMPDIR}/tls.key" \
    --namespace "$NAMESPACE" \
    --dry-run=client -o yaml | kubectl apply -f -

# Re-apply annotations since kubectl apply on the TLS secret replaces metadata.
echo "==> Re-applying acm-sync annotations"
kubectl annotate secret "$SECRET_NAME" \
    --namespace "$NAMESPACE" \
    --overwrite \
    acm-sync.maplarge.com/enabled=true \
    acm-sync.maplarge.com/region=us-east-1 \
    acm-sync.maplarge.com/arn="$CURRENT_ARN" \
    acm-sync.maplarge.com/tags="env=local-dev,managed-by=acm-sync"

echo ""
echo "==> Secret updated. The controller should detect the new hash and re-import to:"
echo "    ARN: $CURRENT_ARN"
echo ""
echo "  Watch the controller logs:"
echo "    kubectl logs -f -n acm-sync deployment/acm-sync"
echo ""
echo "  Verify the update:"
echo "    kubectl get secret ${SECRET_NAME} -n ${NAMESPACE} -o jsonpath='{.metadata.annotations}' | jq ."
echo ""
echo "  The hash should change from:"
echo "    $CURRENT_HASH"
echo "  to a new value, and last-synced-time should update."

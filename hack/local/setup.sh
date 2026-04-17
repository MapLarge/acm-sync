#!/usr/bin/env bash
#
# Sets up a local Kind cluster and deploys acm-sync for development testing.
#
# By default, uses LocalStack with dummy credentials. To test against real AWS,
# pass --aws to inject credentials from your local environment.
#
# Prerequisites: docker, kind, kubectl, helm
#
# Usage:
#   ./hack/local/setup.sh              # LocalStack mode (dummy creds)
#   ./hack/local/setup.sh --aws        # Real AWS mode (reads local creds)
#   ./hack/local/setup.sh rebuild      # rebuild image and redeploy
#   ./hack/local/setup.sh teardown     # destroy everything
#
# Real AWS credential resolution (in order):
#   1. AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN env vars
#   2. AWS CLI profile via AWS_PROFILE (uses `aws configure export-credentials`)
#   3. Default profile in ~/.aws/credentials

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CLUSTER_NAME="acm-sync-dev"
NAMESPACE="acm-sync"
IMAGE="maplarge/acm-sync:dev"
LOCALSTACK_COMPOSE="${REPO_ROOT}/hack/local/docker-compose.yaml"
DEV_VALUES="${REPO_ROOT}/hack/local/dev-values.yaml"
AWS_VALUES="${REPO_ROOT}/hack/local/aws-values.yaml"
USE_REAL_AWS=false

cd "$REPO_ROOT"

# ---------- helpers ----------

log()  { echo "==> $*"; }
warn() { echo "WARN: $*" >&2; }

check_prerequisites() {
    local missing=()
    for cmd in docker kind kubectl helm; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done
    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "ERROR: missing required tools: ${missing[*]}" >&2
        exit 1
    fi
}

# Resolve AWS credentials from the local environment.
# Sets AWS_KEY, AWS_SECRET, and optionally AWS_TOKEN.
resolve_aws_credentials() {
    # 1. Explicit env vars.
    if [[ -n "${AWS_ACCESS_KEY_ID:-}" && -n "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
        log "Using AWS credentials from environment variables"
        AWS_KEY="$AWS_ACCESS_KEY_ID"
        AWS_SECRET="$AWS_SECRET_ACCESS_KEY"
        AWS_TOKEN="${AWS_SESSION_TOKEN:-}"
        return 0
    fi

    # 2. AWS CLI export (handles SSO, profiles, credential_process, etc.)
    if command -v aws &>/dev/null; then
        log "Resolving AWS credentials via AWS CLI (profile: ${AWS_PROFILE:-default})"
        local creds
        if creds="$(aws configure export-credentials --format env-no-export 2>/dev/null)"; then
            AWS_KEY="$(echo "$creds" | grep '^AWS_ACCESS_KEY_ID=' | cut -d= -f2-)"
            AWS_SECRET="$(echo "$creds" | grep '^AWS_SECRET_ACCESS_KEY=' | cut -d= -f2-)"
            AWS_TOKEN="$(echo "$creds" | grep '^AWS_SESSION_TOKEN=' | cut -d= -f2- || true)"
            if [[ -n "$AWS_KEY" && -n "$AWS_SECRET" ]]; then
                return 0
            fi
        fi
    fi

    # 3. Direct file parse as last resort.
    local creds_file="${AWS_SHARED_CREDENTIALS_FILE:-$HOME/.aws/credentials}"
    local profile="${AWS_PROFILE:-default}"
    if [[ -f "$creds_file" ]]; then
        log "Reading credentials from $creds_file [${profile}]"
        AWS_KEY="$(awk -v p="[$profile]" 'found && /aws_access_key_id/{print $3; exit} $0==p{found=1}' "$creds_file")"
        AWS_SECRET="$(awk -v p="[$profile]" 'found && /aws_secret_access_key/{print $3; exit} $0==p{found=1}' "$creds_file")"
        AWS_TOKEN="$(awk -v p="[$profile]" 'found && /aws_session_token/{print $3; exit} $0==p{found=1}' "$creds_file" || true)"
        if [[ -n "$AWS_KEY" && -n "$AWS_SECRET" ]]; then
            return 0
        fi
    fi

    echo "ERROR: could not resolve AWS credentials." >&2
    echo "" >&2
    echo "Options:" >&2
    echo "  - Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY env vars" >&2
    echo "  - Set AWS_PROFILE to a configured profile" >&2
    echo "  - Run 'aws sso login' first if using SSO" >&2
    echo "  - Ensure ~/.aws/credentials has a [default] profile" >&2
    exit 1
}

# Create or update the Kubernetes Secret with AWS credentials.
create_aws_secret() {
    local secret_name="$1"
    local args=(
        --namespace "$NAMESPACE"
        --from-literal=access-key-id="$AWS_KEY"
        --from-literal=secret-access-key="$AWS_SECRET"
    )
    if [[ -n "${AWS_TOKEN:-}" ]]; then
        args+=(--from-literal=session-token="$AWS_TOKEN")
    fi

    kubectl create secret generic "$secret_name" \
        "${args[@]}" \
        --dry-run=client -o yaml | kubectl apply -f -
}

# ---------- teardown ----------

teardown() {
    log "Tearing down local environment"
    docker compose -f "$LOCALSTACK_COMPOSE" down 2>/dev/null || true
    kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
    log "Done"
}

# ---------- rebuild ----------

rebuild() {
    log "Building controller image"
    docker build -t "$IMAGE" "$REPO_ROOT"

    log "Loading image into Kind"
    kind load docker-image "$IMAGE" --name "$CLUSTER_NAME"

    log "Upgrading Helm release"
    local values_file="$DEV_VALUES"
    if [[ "$USE_REAL_AWS" == "true" ]]; then
        values_file="$AWS_VALUES"
    fi
    helm upgrade acm-sync "${REPO_ROOT}/charts/acm-sync" \
        --namespace "$NAMESPACE" \
        --values "$values_file" \
        --wait --timeout 60s

    log "Restarting controller pod"
    kubectl rollout restart deployment/acm-sync -n "$NAMESPACE"
    kubectl rollout status deployment/acm-sync -n "$NAMESPACE" --timeout=60s

    log "Rebuild complete"
}

# ---------- full setup ----------

full_setup() {
    check_prerequisites

    local values_file="$DEV_VALUES"
    local creds_secret="aws-localstack-creds"

    # 1. Create Kind cluster (if not exists).
    if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
        log "Kind cluster '$CLUSTER_NAME' already exists"
    else
        log "Creating Kind cluster '$CLUSTER_NAME'"
        kind create cluster --config "${REPO_ROOT}/hack/local/kind-config.yaml"
    fi

    # 2. Start LocalStack if in local mode, resolve creds if in AWS mode.
    if [[ "$USE_REAL_AWS" == "true" ]]; then
        log "Mode: Real AWS"
        resolve_aws_credentials
        values_file="$AWS_VALUES"
        creds_secret="aws-creds"
    else
        log "Mode: Moto (mock AWS)"
        log "Starting Moto"
        docker compose -f "$LOCALSTACK_COMPOSE" up -d
        AWS_KEY="test"
        AWS_SECRET="test"
        AWS_TOKEN=""
    fi

    # 3. Build and load the controller image.
    log "Building controller image"
    docker build -t "$IMAGE" "$REPO_ROOT"

    log "Loading image into Kind"
    kind load docker-image "$IMAGE" --name "$CLUSTER_NAME"

    # 4. Create namespace.
    kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

    # 5. Create the AWS credentials secret.
    log "Creating AWS credentials secret '${creds_secret}'"
    create_aws_secret "$creds_secret"

    # 6. Deploy with Helm.
    log "Deploying acm-sync via Helm"
    helm upgrade --install acm-sync "${REPO_ROOT}/charts/acm-sync" \
        --namespace "$NAMESPACE" \
        --values "$values_file" \
        --wait --timeout 120s

    # 7. Verify.
    log "Waiting for controller pod to be ready"
    kubectl rollout status deployment/acm-sync -n "$NAMESPACE" --timeout=60s

    echo ""
    log "Local environment is ready!"
    echo ""
    echo "  Cluster:     $CLUSTER_NAME"
    echo "  Namespace:   $NAMESPACE"
    if [[ "$USE_REAL_AWS" == "true" ]]; then
        echo "  AWS:         real credentials (from local environment)"
    else
        echo "  Moto:        http://localhost:5000"
    fi
    echo ""
    echo "  Controller logs:"
    echo "    kubectl logs -f -n $NAMESPACE deployment/acm-sync"
    echo ""
    echo "  Create a test secret:"
    echo "    ./hack/local/create-test-secret.sh"
    echo ""
    echo "  Rebuild after code changes:"
    echo "    ./hack/local/setup.sh rebuild"
    echo ""
    if [[ "$USE_REAL_AWS" == "true" ]]; then
        echo "  Refresh credentials (e.g., after SSO token expires):"
        echo "    ./hack/local/setup.sh refresh-creds"
        echo ""
    fi
    echo "  Tear down:"
    echo "    ./hack/local/setup.sh teardown"
}

# ---------- refresh-creds ----------

refresh_creds() {
    resolve_aws_credentials
    log "Updating AWS credentials secret"
    create_aws_secret "aws-creds"
    log "Restarting controller pod to pick up new credentials"
    kubectl rollout restart deployment/acm-sync -n "$NAMESPACE"
    kubectl rollout status deployment/acm-sync -n "$NAMESPACE" --timeout=60s
    log "Credentials refreshed"
}

# ---------- main ----------

# Parse flags.
args=()
for arg in "$@"; do
    case "$arg" in
        --aws) USE_REAL_AWS=true ;;
        *)     args+=("$arg") ;;
    esac
done
set -- "${args[@]+"${args[@]}"}"

case "${1:-}" in
    teardown)      teardown ;;
    rebuild)       rebuild ;;
    refresh-creds) refresh_creds ;;
    *)             full_setup ;;
esac

# Decisions

Ambiguities encountered during implementation that CLAUDE.md did not fully resolve.

## 1. JSON Patch vs Strategic Merge Patch for annotation updates

**Decision:** Use JSON Patch (RFC 6902) via `types.JSONPatchType`.

**Reasoning:** The task spec explicitly requires JSON patch, noting that Secrets don't have a merge strategy for annotations and we want predictable semantics. JSON Patch gives us explicit add/replace/remove operations per annotation key rather than relying on Kubernetes merge behavior.

## 2. Multi-region ARN writeback

**Decision:** When a Secret has multiple regions and no ARN annotation, the controller imports to each region and writes back **the last region's ARN** as the `arn` annotation.

**Reasoning:** CLAUDE.md says "If absent: call ImportCertificate without ARN, receive new ARN, patch Secret annotation with returned ARN." It doesn't specify multi-region semantics for the ARN writeback. Since each region returns a different ARN, and the annotation only holds one value, we write the last one. This is a known limitation — multi-region fan-out with per-region ARN tracking would require a more complex annotation scheme (e.g., `arn/us-east-1`, `arn/us-west-2`). Flagging for review.

## 3. Change detection when no ARN annotation exists yet

**Decision:** If the hash matches `last-synced-hash` but there's no ARN annotation (first import hasn't happened yet), we **do not skip** — we proceed with the import.

**Reasoning:** The skip condition checks `hash == status.LastSyncedHash && input.ARN != "" && input.ARN == status.LastSyncedARN`. Requiring a non-empty ARN ensures we don't skip the initial import even if someone pre-populates the hash annotation.

## 4. Events API version

**Decision:** Use `k8s.io/client-go/tools/events.EventRecorder` (new API) instead of `k8s.io/client-go/tools/record.EventRecorder` (deprecated).

**Reasoning:** The old `GetEventRecorderFor` is deprecated in controller-runtime v0.23. Using the new `GetEventRecorder` avoids staticcheck warnings and is forward-compatible. The new API requires an `action` parameter in addition to `reason`, which maps naturally to our operations (ImportCertificate, Reconcile).

## 5. Certificate chain splitting

**Decision:** The first PEM block in `tls.crt` is treated as the leaf certificate, and all subsequent blocks are passed as the certificate chain to ACM.

**Reasoning:** This matches the convention used by cert-manager and most TLS tooling. ACM's ImportCertificate API expects the leaf and chain separately.

## 6. Partition validation in annotations package

**Decision:** `PartitionFromRegion` uses prefix matching (`us-gov-` → `aws-us-gov`, `cn-` → `aws-cn`, default → `aws`) rather than calling the AWS SDK.

**Reasoning:** The annotations package has a "no AWS imports" constraint. Prefix matching covers all known partitions and avoids introducing an SDK dependency into a pure validation package.

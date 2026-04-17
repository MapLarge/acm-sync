package annotations

import (
	"errors"
	"fmt"
	"strings"
)

const (
	Prefix = "acm-sync.maplarge.com/"

	// Input annotations (set by user on Secret).
	KeyEnabled = Prefix + "enabled"
	KeyRegion  = Prefix + "region"
	KeyARN     = Prefix + "arn"
	KeyTags    = Prefix + "tags"

	// Status annotations (set by controller).
	KeyLastSyncedARN  = Prefix + "last-synced-arn"
	KeyLastSyncedTime = Prefix + "last-synced-time"
	KeyLastSyncedHash = Prefix + "last-synced-hash"
	KeyLastError      = Prefix + "last-error"

	// ACM tag limits.
	maxTagKeyLen   = 128
	maxTagValueLen = 256
)

// ErrNotEnabled is returned when the Secret does not have the enabled annotation set to "true".
var ErrNotEnabled = errors.New("secret is not opted in to acm-sync")

// Input holds the parsed and validated input annotations from a Secret.
type Input struct {
	Regions []string
	ARN     string
	Tags    map[string]string
}

// Status holds the status annotations written by the controller.
type Status struct {
	LastSyncedARN  string
	LastSyncedTime string
	LastSyncedHash string
	LastError      string
}

// Parse extracts and validates acm-sync annotations from a map.
// Returns ErrNotEnabled if the secret is not opted in.
func Parse(annotations map[string]string) (Input, error) {
	if annotations == nil || annotations[KeyEnabled] != "true" {
		return Input{}, ErrNotEnabled
	}

	regionRaw, ok := annotations[KeyRegion]
	if !ok || strings.TrimSpace(regionRaw) == "" {
		return Input{}, fmt.Errorf("annotation %q is required", KeyRegion)
	}

	regions := parseCSV(regionRaw)
	if len(regions) == 0 {
		return Input{}, fmt.Errorf("annotation %q must contain at least one region", KeyRegion)
	}
	for _, r := range regions {
		if err := validateRegion(r); err != nil {
			return Input{}, err
		}
	}

	input := Input{
		Regions: regions,
		ARN:     annotations[KeyARN],
	}

	if input.ARN != "" {
		if err := validateARN(input.ARN); err != nil {
			return Input{}, err
		}
	}

	if tagsRaw, ok := annotations[KeyTags]; ok && tagsRaw != "" {
		tags, err := parseTags(tagsRaw)
		if err != nil {
			return Input{}, err
		}
		input.Tags = tags
	}

	return input, nil
}

// ParseStatus extracts status annotations from a map.
func ParseStatus(annotations map[string]string) Status {
	if annotations == nil {
		return Status{}
	}
	return Status{
		LastSyncedARN:  annotations[KeyLastSyncedARN],
		LastSyncedTime: annotations[KeyLastSyncedTime],
		LastSyncedHash: annotations[KeyLastSyncedHash],
		LastError:      annotations[KeyLastError],
	}
}

// StatusAnnotations converts a Status to a map of annotations suitable for patching.
func StatusAnnotations(s Status) map[string]string {
	m := map[string]string{
		KeyLastSyncedARN:  s.LastSyncedARN,
		KeyLastSyncedTime: s.LastSyncedTime,
		KeyLastSyncedHash: s.LastSyncedHash,
	}
	if s.LastError != "" {
		m[KeyLastError] = s.LastError
	} else {
		// Empty string signals "clear this annotation" — callers can use this
		// to remove last-error on success.
		m[KeyLastError] = ""
	}
	return m
}

func parseCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func validateRegion(region string) error {
	// Minimal validation: regions are lowercase alphanumeric with hyphens and digits.
	if len(region) < 5 || len(region) > 25 {
		return fmt.Errorf("invalid region %q: unexpected length", region)
	}
	for _, c := range region {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("invalid region %q: contains invalid character %q", region, c)
		}
	}
	return nil
}

func validateARN(arn string) error {
	// ARN format: arn:<partition>:acm:<region>:<account>:certificate/<id>
	if !strings.HasPrefix(arn, "arn:") {
		return fmt.Errorf("invalid ARN %q: must start with 'arn:'", arn)
	}
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return fmt.Errorf("invalid ARN %q: expected at least 6 colon-separated fields", arn)
	}
	if parts[2] != "acm" {
		return fmt.Errorf("invalid ARN %q: service must be 'acm', got %q", arn, parts[2])
	}
	return nil
}

// ValidateARNPartitionForRegions checks that the partition in the ARN is consistent
// with the target regions. This prevents cross-partition sync attempts.
func ValidateARNPartitionForRegions(arn string, regions []string) error {
	if arn == "" {
		return nil
	}
	arnPartition := partitionFromARN(arn)
	for _, region := range regions {
		regionPartition := PartitionFromRegion(region)
		if arnPartition != regionPartition {
			return fmt.Errorf(
				"ARN partition %q does not match region %q partition %q: cross-partition sync is not supported",
				arnPartition, region, regionPartition,
			)
		}
	}
	return nil
}

func partitionFromARN(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// PartitionFromRegion determines the AWS partition for a given region.
func PartitionFromRegion(region string) string {
	if strings.HasPrefix(region, "us-gov-") {
		return "aws-us-gov"
	}
	if strings.HasPrefix(region, "cn-") {
		return "aws-cn"
	}
	return "aws"
}

func parseTags(raw string) (map[string]string, error) {
	tags := make(map[string]string)
	for _, pair := range parseCSV(raw) {
		idx := strings.Index(pair, "=")
		if idx < 0 {
			return nil, fmt.Errorf("invalid tag %q: must be key=value", pair)
		}
		key := pair[:idx]
		value := pair[idx+1:]

		if key == "" {
			return nil, fmt.Errorf("invalid tag %q: key cannot be empty", pair)
		}
		if strings.HasPrefix(key, "aws:") {
			return nil, fmt.Errorf("invalid tag key %q: keys starting with 'aws:' are reserved", key)
		}
		if len(key) > maxTagKeyLen {
			return nil, fmt.Errorf("tag key %q exceeds maximum length of %d", key, maxTagKeyLen)
		}
		if len(value) > maxTagValueLen {
			return nil, fmt.Errorf("tag value for key %q exceeds maximum length of %d", key, maxTagValueLen)
		}
		tags[key] = value
	}
	return tags, nil
}

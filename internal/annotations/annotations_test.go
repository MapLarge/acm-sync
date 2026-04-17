package annotations

import (
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantInput   Input
		wantErr     error
		wantErrMsg  string
	}{
		{
			name:        "nil annotations returns ErrNotEnabled",
			annotations: nil,
			wantErr:     ErrNotEnabled,
		},
		{
			name:        "empty annotations returns ErrNotEnabled",
			annotations: map[string]string{},
			wantErr:     ErrNotEnabled,
		},
		{
			name: "enabled=false returns ErrNotEnabled",
			annotations: map[string]string{
				KeyEnabled: "false",
			},
			wantErr: ErrNotEnabled,
		},
		{
			name: "enabled=TRUE (wrong case) returns ErrNotEnabled",
			annotations: map[string]string{
				KeyEnabled: "TRUE",
			},
			wantErr: ErrNotEnabled,
		},
		{
			name: "enabled without region returns error",
			annotations: map[string]string{
				KeyEnabled: "true",
			},
			wantErrMsg: "required",
		},
		{
			name: "enabled with empty region returns error",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "",
			},
			wantErrMsg: "required",
		},
		{
			name: "enabled with whitespace-only region returns error",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "  ,  ",
			},
			wantErrMsg: "at least one region",
		},
		{
			name: "single region parses correctly",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
			},
			wantInput: Input{
				Regions: []string{"us-east-1"},
			},
		},
		{
			name: "multi-region parses correctly",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1, us-west-2, us-gov-west-1",
			},
			wantInput: Input{
				Regions: []string{"us-east-1", "us-west-2", "us-gov-west-1"},
			},
		},
		{
			name: "invalid region with uppercase",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "US-EAST-1",
			},
			wantErrMsg: "invalid character",
		},
		{
			name: "region too short",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-1",
			},
			wantErrMsg: "unexpected length",
		},
		{
			name: "with valid ARN",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyARN:     "arn:aws:acm:us-east-1:123456789012:certificate/abc-123",
			},
			wantInput: Input{
				Regions: []string{"us-east-1"},
				ARN:     "arn:aws:acm:us-east-1:123456789012:certificate/abc-123",
			},
		},
		{
			name: "ARN without arn: prefix",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyARN:     "notanarn",
			},
			wantErrMsg: "must start with 'arn:'",
		},
		{
			name: "ARN with wrong service",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyARN:     "arn:aws:s3:us-east-1:123456789012:bucket/foo",
			},
			wantErrMsg: "service must be 'acm'",
		},
		{
			name: "ARN with too few fields",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyARN:     "arn:aws:acm",
			},
			wantErrMsg: "at least 6",
		},
		{
			name: "valid tags",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "env=prod,team=platform",
			},
			wantInput: Input{
				Regions: []string{"us-east-1"},
				Tags:    map[string]string{"env": "prod", "team": "platform"},
			},
		},
		{
			name: "tag with empty value is allowed",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "env=",
			},
			wantInput: Input{
				Regions: []string{"us-east-1"},
				Tags:    map[string]string{"env": ""},
			},
		},
		{
			name: "tag missing equals sign",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "novaluehere",
			},
			wantErrMsg: "must be key=value",
		},
		{
			name: "tag with empty key",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "=value",
			},
			wantErrMsg: "key cannot be empty",
		},
		{
			name: "tag key starting with aws:",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "aws:internal=foo",
			},
			wantErrMsg: "keys starting with 'aws:' are reserved",
		},
		{
			name: "tag key exceeds max length",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    strings.Repeat("k", 129) + "=v",
			},
			wantErrMsg: "exceeds maximum length of 128",
		},
		{
			name: "tag value exceeds max length",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "key=" + strings.Repeat("v", 257),
			},
			wantErrMsg: "exceeds maximum length of 256",
		},
		{
			name: "tag key at exact max length is allowed",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    strings.Repeat("k", 128) + "=v",
			},
			wantInput: Input{
				Regions: []string{"us-east-1"},
				Tags:    map[string]string{strings.Repeat("k", 128): "v"},
			},
		},
		{
			name: "tag value at exact max length is allowed",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "key=" + strings.Repeat("v", 256),
			},
			wantInput: Input{
				Regions: []string{"us-east-1"},
				Tags:    map[string]string{"key": strings.Repeat("v", 256)},
			},
		},
		{
			name: "empty tags annotation is a no-op",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-east-1",
				KeyTags:    "",
			},
			wantInput: Input{
				Regions: []string{"us-east-1"},
			},
		},
		{
			name: "GovCloud region",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-gov-west-1",
			},
			wantInput: Input{
				Regions: []string{"us-gov-west-1"},
			},
		},
		{
			name: "GovCloud ARN with GovCloud region",
			annotations: map[string]string{
				KeyEnabled: "true",
				KeyRegion:  "us-gov-west-1",
				KeyARN:     "arn:aws-us-gov:acm:us-gov-west-1:123456789012:certificate/abc-123",
			},
			wantInput: Input{
				Regions: []string{"us-gov-west-1"},
				ARN:     "arn:aws-us-gov:acm:us-gov-west-1:123456789012:certificate/abc-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.annotations)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantErrMsg != "" {
				if err == nil {
					t.Fatal("Parse() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("Parse() error = %q, want substring %q", err.Error(), tt.wantErrMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}

			if len(got.Regions) != len(tt.wantInput.Regions) {
				t.Errorf("Regions = %v, want %v", got.Regions, tt.wantInput.Regions)
			} else {
				for i, r := range got.Regions {
					if r != tt.wantInput.Regions[i] {
						t.Errorf("Regions[%d] = %q, want %q", i, r, tt.wantInput.Regions[i])
					}
				}
			}
			if got.ARN != tt.wantInput.ARN {
				t.Errorf("ARN = %q, want %q", got.ARN, tt.wantInput.ARN)
			}
			if len(got.Tags) != len(tt.wantInput.Tags) {
				t.Errorf("Tags = %v, want %v", got.Tags, tt.wantInput.Tags)
			} else {
				for k, v := range tt.wantInput.Tags {
					if got.Tags[k] != v {
						t.Errorf("Tags[%q] = %q, want %q", k, got.Tags[k], v)
					}
				}
			}
		})
	}
}

func TestValidateARNPartitionForRegions(t *testing.T) {
	tests := []struct {
		name       string
		arn        string
		regions    []string
		wantErrMsg string
	}{
		{
			name:    "empty ARN is always valid",
			arn:     "",
			regions: []string{"us-east-1"},
		},
		{
			name:    "commercial ARN with commercial region",
			arn:     "arn:aws:acm:us-east-1:123456789012:certificate/abc",
			regions: []string{"us-east-1", "us-west-2"},
		},
		{
			name:    "govcloud ARN with govcloud region",
			arn:     "arn:aws-us-gov:acm:us-gov-west-1:123456789012:certificate/abc",
			regions: []string{"us-gov-west-1"},
		},
		{
			name:       "commercial ARN with govcloud region",
			arn:        "arn:aws:acm:us-east-1:123456789012:certificate/abc",
			regions:    []string{"us-gov-west-1"},
			wantErrMsg: "cross-partition sync is not supported",
		},
		{
			name:       "govcloud ARN with commercial region",
			arn:        "arn:aws-us-gov:acm:us-gov-west-1:123456789012:certificate/abc",
			regions:    []string{"us-east-1"},
			wantErrMsg: "cross-partition sync is not supported",
		},
		{
			name:       "mixed partitions in regions with commercial ARN",
			arn:        "arn:aws:acm:us-east-1:123456789012:certificate/abc",
			regions:    []string{"us-east-1", "us-gov-west-1"},
			wantErrMsg: "cross-partition sync is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateARNPartitionForRegions(tt.arn, tt.regions)
			if tt.wantErrMsg != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        Status
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			want:        Status{},
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			want:        Status{},
		},
		{
			name: "all status fields present",
			annotations: map[string]string{
				KeyLastSyncedARN:  "arn:aws:acm:us-east-1:123456789012:certificate/abc",
				KeyLastSyncedTime: "2026-01-01T00:00:00Z",
				KeyLastSyncedHash: "abc123",
				KeyLastError:      "some error",
			},
			want: Status{
				LastSyncedARN:  "arn:aws:acm:us-east-1:123456789012:certificate/abc",
				LastSyncedTime: "2026-01-01T00:00:00Z",
				LastSyncedHash: "abc123",
				LastError:      "some error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseStatus(tt.annotations)
			if got != tt.want {
				t.Errorf("ParseStatus() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStatusAnnotations(t *testing.T) {
	s := Status{
		LastSyncedARN:  "arn:aws:acm:us-east-1:123456789012:certificate/abc",
		LastSyncedTime: "2026-01-01T00:00:00Z",
		LastSyncedHash: "abc123",
	}
	m := StatusAnnotations(s)
	if m[KeyLastSyncedARN] != s.LastSyncedARN {
		t.Errorf("unexpected ARN: %q", m[KeyLastSyncedARN])
	}
	if m[KeyLastSyncedTime] != s.LastSyncedTime {
		t.Errorf("unexpected time: %q", m[KeyLastSyncedTime])
	}
	if m[KeyLastSyncedHash] != s.LastSyncedHash {
		t.Errorf("unexpected hash: %q", m[KeyLastSyncedHash])
	}
	if m[KeyLastError] != "" {
		t.Errorf("expected empty last-error, got %q", m[KeyLastError])
	}

	// With error
	s.LastError = "oops"
	m = StatusAnnotations(s)
	if m[KeyLastError] != "oops" {
		t.Errorf("expected last-error 'oops', got %q", m[KeyLastError])
	}
}

func TestPartitionFromRegion(t *testing.T) {
	tests := []struct {
		region string
		want   string
	}{
		{"us-east-1", "aws"},
		{"us-west-2", "aws"},
		{"eu-west-1", "aws"},
		{"us-gov-west-1", "aws-us-gov"},
		{"us-gov-east-1", "aws-us-gov"},
		{"cn-north-1", "aws-cn"},
		{"cn-northwest-1", "aws-cn"},
	}
	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			got := PartitionFromRegion(tt.region)
			if got != tt.want {
				t.Errorf("PartitionFromRegion(%q) = %q, want %q", tt.region, got, tt.want)
			}
		})
	}
}

package s3kit

import "testing"

func TestSnowballAutoExtractMetadata(t *testing.T) {
	t.Parallel()

	got := snowballAutoExtractMetadata()
	if len(got) != 1 {
		t.Fatalf("expected one metadata entry, got %d", len(got))
	}
	if got[snowballAutoExtractMetadataKey] != "true" {
		t.Fatalf("expected snowball auto extract metadata to be true, got %#v", got)
	}
	if _, ok := got["x-amz-meta-snowball-auto-extract"]; ok {
		t.Fatal("metadata key must not include x-amz-meta prefix")
	}
}

func TestSnowballAutoExtractMetadataReturnsFreshMap(t *testing.T) {
	t.Parallel()

	first := snowballAutoExtractMetadata()
	first[snowballAutoExtractMetadataKey] = "false"

	second := snowballAutoExtractMetadata()
	if second[snowballAutoExtractMetadataKey] != "true" {
		t.Fatalf("expected fresh metadata map, got %#v", second)
	}
}

func TestArchiveTransferOptionsDefaultModeIsPortable(t *testing.T) {
	t.Parallel()

	got := ArchiveTransferOptions{}.normalized()
	if got.Mode != ArchiveExtractPortable {
		t.Fatalf("expected default mode %q, got %q", ArchiveExtractPortable, got.Mode)
	}
}

func TestValidateArchiveTransferOptions(t *testing.T) {
	t.Parallel()

	valid := ArchiveTransferOptions{
		SourceBucket:          "source",
		SourceKey:             "archives/data.tar",
		DestinationBucket:     "destination",
		DestinationArchiveKey: "imports/data.tar",
	}

	tests := []struct {
		name        string
		source      *Client
		destination *Client
		opts        ArchiveTransferOptions
		wantErr     bool
	}{
		{
			name:        "valid",
			source:      &Client{},
			destination: &Client{},
			opts:        valid,
		},
		{
			name:        "missing source client",
			destination: &Client{},
			opts:        valid,
			wantErr:     true,
		},
		{
			name:        "source url does not require source client or bucket",
			destination: &Client{},
			opts: ArchiveTransferOptions{
				SourceURL:             "https://example.com/archive.zip",
				DestinationBucket:     valid.DestinationBucket,
				DestinationArchiveKey: valid.DestinationArchiveKey,
			},
		},
		{
			name:    "missing destination client",
			source:  &Client{},
			opts:    valid,
			wantErr: true,
		},
		{
			name:        "missing source bucket",
			source:      &Client{},
			destination: &Client{},
			opts: ArchiveTransferOptions{
				SourceKey:             valid.SourceKey,
				DestinationBucket:     valid.DestinationBucket,
				DestinationArchiveKey: valid.DestinationArchiveKey,
			},
			wantErr: true,
		},
		{
			name:        "missing destination bucket",
			source:      &Client{},
			destination: &Client{},
			opts: ArchiveTransferOptions{
				SourceBucket: valid.SourceBucket,
				SourceKey:    valid.SourceKey,
			},
			wantErr: true,
		},
		{
			name:        "snowball mode requires archive key",
			source:      &Client{},
			destination: &Client{},
			opts: ArchiveTransferOptions{
				SourceBucket:      valid.SourceBucket,
				SourceKey:         valid.SourceKey,
				DestinationBucket: valid.DestinationBucket,
				Mode:              ArchiveExtractMinIOSnowball,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateArchiveTransferOptions(tt.source, tt.destination, tt.opts.normalized())
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%t, got err=%v", tt.wantErr, err)
			}
		})
	}
}

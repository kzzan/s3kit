package s3kit

import (
	"errors"
	"testing"
)

func TestArchiveEntryObjectKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prefix    string
		entryName string
		want      string
		wantErr   error
	}{
		{
			name:      "plain file",
			entryName: "hello.txt",
			want:      "hello.txt",
		},
		{
			name:      "nested file with prefix",
			prefix:    "imports/run-1/",
			entryName: "dir/hello.txt",
			want:      "imports/run-1/dir/hello.txt",
		},
		{
			name:      "windows separator normalized",
			prefix:    "/imports/run-1/",
			entryName: `dir\hello.txt`,
			want:      "imports/run-1/dir/hello.txt",
		},
		{
			name:      "empty entry rejected",
			entryName: "",
			wantErr:   ErrInvalidArchiveEntryName,
		},
		{
			name:      "absolute entry rejected",
			entryName: "/etc/passwd",
			wantErr:   ErrInvalidArchiveEntryName,
		},
		{
			name:      "parent segment rejected",
			entryName: "../secret.txt",
			wantErr:   ErrInvalidArchiveEntryName,
		},
		{
			name:      "windows parent segment rejected",
			entryName: `..\secret.txt`,
			wantErr:   ErrInvalidArchiveEntryName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := archiveEntryObjectKey(tt.prefix, tt.entryName)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if got != tt.want {
				t.Fatalf("expected key %q, got %q", tt.want, got)
			}
		})
	}
}

func TestDetectArchiveFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    ArchiveTransferOptions
		want    ArchiveFormat
		wantErr error
	}{
		{
			name: "zip source key",
			opts: ArchiveTransferOptions{SourceKey: "data/archive.zip"},
			want: ArchiveFormatZip,
		},
		{
			name: "tar source url",
			opts: ArchiveTransferOptions{SourceURL: "https://example.com/export.tar?token=abc"},
			want: ArchiveFormatTar,
		},
		{
			name: "tgz source key",
			opts: ArchiveTransferOptions{SourceKey: "data/archive.tgz"},
			want: ArchiveFormatTarGz,
		},
		{
			name: "explicit format",
			opts: ArchiveTransferOptions{SourceKey: "data/archive.bin", Format: ArchiveFormatZip},
			want: ArchiveFormatZip,
		},
		{
			name:    "unsupported",
			opts:    ArchiveTransferOptions{SourceKey: "data/archive.rar"},
			wantErr: ErrUnsupportedArchive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := detectArchiveFormat(tt.opts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if got != tt.want {
				t.Fatalf("expected format %q, got %q", tt.want, got)
			}
		})
	}
}

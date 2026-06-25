package s3kit

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestCopySourceEscapesKeys(t *testing.T) {
	t.Parallel()

	got := copySource("bucket", "folder/hello world.txt", "")
	want := url.PathEscape("bucket/folder/hello world.txt")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCopySourceEscapesVersionID(t *testing.T) {
	t.Parallel()

	got := copySource("bucket", "folder/hello world.txt", "version+123")
	want := url.PathEscape("bucket/folder/hello world.txt") + "?versionId=" + url.QueryEscape("version+123")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestObjectInputSetsVersionID(t *testing.T) {
	t.Parallel()

	got := objectInput("bucket", "key", "version-123")
	if got.VersionId == nil {
		t.Fatal("expected version id to be set")
	}
	if want := "version-123"; *got.VersionId != want {
		t.Fatalf("expected version id %q, got %q", want, *got.VersionId)
	}
}

func TestObjectInputOmitsVersionIDWhenEmpty(t *testing.T) {
	t.Parallel()

	got := objectInput("bucket", "key", "")
	if got.VersionId != nil {
		t.Fatalf("expected version id to be omitted, got %q", *got.VersionId)
	}
}

func TestHeadObjectInputSetsVersionID(t *testing.T) {
	t.Parallel()

	got := headObjectInput("bucket", "key", "version-123")
	if got.VersionId == nil {
		t.Fatal("expected version id to be set")
	}
	if want := "version-123"; *got.VersionId != want {
		t.Fatalf("expected version id %q, got %q", want, *got.VersionId)
	}
}

func TestHeadObjectInputOmitsVersionIDWhenEmpty(t *testing.T) {
	t.Parallel()

	got := headObjectInput("bucket", "key", "")
	if got.VersionId != nil {
		t.Fatalf("expected version id to be omitted, got %q", *got.VersionId)
	}
}

func TestDeleteObjectInputSetsVersionID(t *testing.T) {
	t.Parallel()

	got := deleteObjectInput("bucket", "key", "version-123")
	if got.VersionId == nil {
		t.Fatal("expected version id to be set")
	}
	if want := "version-123"; *got.VersionId != want {
		t.Fatalf("expected version id %q, got %q", want, *got.VersionId)
	}
}

func TestDeleteObjectInputOmitsVersionIDWhenEmpty(t *testing.T) {
	t.Parallel()

	got := deleteObjectInput("bucket", "key", "")
	if got.VersionId != nil {
		t.Fatalf("expected version id to be omitted, got %q", *got.VersionId)
	}
}

func TestDownloadObjectInputSetsVersionID(t *testing.T) {
	t.Parallel()

	got := downloadObjectInput("bucket", "key", nil, "version-123")
	if got.VersionID == nil {
		t.Fatal("expected version id to be set")
	}
	if want := "version-123"; *got.VersionID != want {
		t.Fatalf("expected version id %q, got %q", want, *got.VersionID)
	}
}

func TestDownloadObjectInputOmitsVersionIDWhenEmpty(t *testing.T) {
	t.Parallel()

	got := downloadObjectInput("bucket", "key", nil, "")
	if got.VersionID != nil {
		t.Fatalf("expected version id to be omitted, got %q", *got.VersionID)
	}
}

func TestObjectVersionIdentifiers(t *testing.T) {
	t.Parallel()

	got := objectVersionIdentifiers([]ObjectVersionIdentifier{
		{
			Key:       "folder/a.txt",
			VersionID: "v1",
		},
		{
			Key:       "folder/b.txt",
			VersionID: "v2",
		},
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 identifiers, got %d", len(got))
	}
	if want := "folder/a.txt"; aws.ToString(got[0].Key) != want {
		t.Fatalf("expected key %q, got %q", want, aws.ToString(got[0].Key))
	}
	if want := "v1"; aws.ToString(got[0].VersionId) != want {
		t.Fatalf("expected version id %q, got %q", want, aws.ToString(got[0].VersionId))
	}
}

func TestAppendMatchingObjectVersions(t *testing.T) {
	t.Parallel()

	lastModified := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	got := appendMatchingObjectVersions(nil, "folder/file.txt", []types.ObjectVersion{
		{
			Key:          aws.String("folder/file.txt"),
			VersionId:    aws.String("v3"),
			IsLatest:     aws.Bool(true),
			LastModified: aws.Time(lastModified),
			Size:         aws.Int64(12),
			ETag:         aws.String(`"etag-3"`),
		},
		{
			Key:       aws.String("folder/file.txt.bak"),
			VersionId: aws.String("ignored"),
		},
		{
			Key:       aws.String("folder/file.txt"),
			VersionId: aws.String("v2"),
			Size:      aws.Int64(8),
		},
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(got))
	}
	tests := []struct {
		name          string
		index         int
		versionNumber int
		versionID     string
		isLatest      bool
		size          int64
	}{
		{
			name:          "latest exact key",
			index:         0,
			versionNumber: 1,
			versionID:     "v3",
			isLatest:      true,
			size:          12,
		},
		{
			name:          "previous exact key",
			index:         1,
			versionNumber: 2,
			versionID:     "v2",
			isLatest:      false,
			size:          8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			version := got[tt.index]
			if version.VersionNumber != tt.versionNumber {
				t.Fatalf("expected version number %d, got %d", tt.versionNumber, version.VersionNumber)
			}
			if version.VersionID != tt.versionID {
				t.Fatalf("expected version id %q, got %q", tt.versionID, version.VersionID)
			}
			if version.IsLatest != tt.isLatest {
				t.Fatalf("expected is latest %t, got %t", tt.isLatest, version.IsLatest)
			}
			if version.Size != tt.size {
				t.Fatalf("expected size %d, got %d", tt.size, version.Size)
			}
		})
	}

	if got[0].LastModified != lastModified {
		t.Fatalf("expected last modified %s, got %s", lastModified, got[0].LastModified)
	}
	if want := `"etag-3"`; got[0].ETag != want {
		t.Fatalf("expected etag %q, got %q", want, got[0].ETag)
	}
}

func TestAppendObjectVersionIdentifiersFiltersExactKey(t *testing.T) {
	t.Parallel()

	got := appendObjectVersionIdentifiers(nil, []types.ObjectVersion{
		{
			Key:       aws.String("folder/file.txt"),
			VersionId: aws.String("v1"),
		},
		{
			Key:       aws.String("folder/file.txt.bak"),
			VersionId: aws.String("ignored"),
		},
	}, true, "folder/file.txt")

	if len(got) != 1 {
		t.Fatalf("expected 1 identifier, got %d", len(got))
	}
	if want := "v1"; got[0].VersionID != want {
		t.Fatalf("expected version id %q, got %q", want, got[0].VersionID)
	}
}

func TestAppendDeleteMarkerIdentifiersFiltersExactKey(t *testing.T) {
	t.Parallel()

	got := appendDeleteMarkerIdentifiers(nil, []types.DeleteMarkerEntry{
		{
			Key:       aws.String("folder/file.txt"),
			VersionId: aws.String("marker-1"),
		},
		{
			Key:       aws.String("folder/file.txt.bak"),
			VersionId: aws.String("ignored"),
		},
	}, true, "folder/file.txt")

	if len(got) != 1 {
		t.Fatalf("expected 1 identifier, got %d", len(got))
	}
	if want := "marker-1"; got[0].VersionID != want {
		t.Fatalf("expected version id %q, got %q", want, got[0].VersionID)
	}
}

func TestValidateVersionNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		versionNumber int
		wantErr       error
	}{
		{
			name:          "zero",
			versionNumber: 0,
			wantErr:       ErrInvalidVersionNumber,
		},
		{
			name:          "negative",
			versionNumber: -1,
			wantErr:       ErrInvalidVersionNumber,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := validateVersionNumber(tt.versionNumber); !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestJoinDeleteErrors(t *testing.T) {
	t.Parallel()

	err := joinDeleteErrors([]types.Error{
		{
			Key:     aws.String("folder/a.txt"),
			Code:    aws.String("AccessDenied"),
			Message: aws.String("permission denied"),
		},
		{
			Key:     aws.String("folder/b.txt"),
			Message: aws.String("checksum mismatch"),
		},
		{
			Key:       aws.String("folder/c.txt"),
			VersionId: aws.String("v3"),
			Code:      aws.String("InvalidVersion"),
			Message:   aws.String("missing version"),
		},
	})
	if err == nil {
		t.Fatal("expected joined error")
	}

	got := err.Error()
	wantParts := []string{
		`delete "folder/a.txt" failed with AccessDenied: permission denied`,
		`delete "folder/b.txt" failed: checksum mismatch`,
		`delete "folder/c.txt" version "v3" failed with InvalidVersion: missing version`,
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}
}

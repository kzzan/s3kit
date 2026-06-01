package s3kit

import (
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestCopySourceEscapesKeys(t *testing.T) {
	t.Parallel()

	got := copySource("bucket", "folder/hello world.txt")
	want := url.PathEscape("bucket/folder/hello world.txt")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
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
	})
	if err == nil {
		t.Fatal("expected joined error")
	}

	got := err.Error()
	wantParts := []string{
		`delete "folder/a.txt" failed with AccessDenied: permission denied`,
		`delete "folder/b.txt" failed: checksum mismatch`,
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}
}

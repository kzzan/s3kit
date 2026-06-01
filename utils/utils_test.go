package utils

import "testing"

func TestDetectContentType(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"photo.JPG":   "image/jpeg",
		"doc.json":    "application/json",
		"archive.abc": "application/octet-stream",
	}

	for filename, want := range tests {
		if got := DetectContentType(filename); got != want {
			t.Fatalf("%s: expected %q, got %q", filename, want, got)
		}
	}
}

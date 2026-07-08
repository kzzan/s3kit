package s3kit

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kzzan/s3kit/utils"
)

type ArchiveExtractMode string

const (
	// ArchiveExtractPortable extracts the archive in the application and uploads
	// each file as a normal object. It works with AWS S3, MinIO, RustFS, and
	// generic S3-compatible targets.
	ArchiveExtractPortable ArchiveExtractMode = "portable"
	// ArchiveExtractMinIOSnowball uploads the archive with MinIO's
	// x-amz-meta-snowball-auto-extract metadata. It is faster for MinIO bulk
	// imports but is not portable to AWS S3 or RustFS.
	ArchiveExtractMinIOSnowball ArchiveExtractMode = "minio-snowball"
)

type ArchiveFormat string

const (
	ArchiveFormatAuto  ArchiveFormat = ""
	ArchiveFormatZip   ArchiveFormat = "zip"
	ArchiveFormatTar   ArchiveFormat = "tar"
	ArchiveFormatTarGz ArchiveFormat = "tar.gz"
)

var (
	ErrInvalidArchiveEntryName = errors.New("s3kit: invalid archive entry name")
	ErrArchiveEntryLimit       = errors.New("s3kit: archive entry limit exceeded")
	ErrArchiveSizeLimit        = errors.New("s3kit: archive uncompressed size limit exceeded")
	ErrUnsupportedArchive      = errors.New("s3kit: unsupported archive format")
)

const maxInt64 = int64(1<<63 - 1)

// ArchiveTransferOptions configures TransferArchiveAndExtract.
type ArchiveTransferOptions struct {
	// SourceBucket and SourceKey identify the source object when SourceURL is
	// empty.
	SourceBucket string
	SourceKey    string
	// SourceURL is an optional HTTP(S) or presigned S3 URL. When set, SourceBucket
	// is not required and source may be nil.
	SourceURL string
	// DestinationBucket receives the extracted objects. DestinationArchiveKey is
	// optional in portable mode and required in MinIO Snowball mode.
	DestinationBucket     string
	DestinationArchiveKey string
	// ExtractPrefix is prepended to extracted object keys in portable mode.
	ExtractPrefix string
	// Mode defaults to ArchiveExtractPortable for cross-provider compatibility.
	Mode ArchiveExtractMode
	// Format defaults to auto-detection from SourceKey, SourceURL, or
	// DestinationArchiveKey.
	Format ArchiveFormat
	// ContentType optionally overrides the uploaded archive content type in
	// MinIO Snowball mode.
	ContentType string
	// TempDir optionally selects where portable mode stores the temporary archive.
	TempDir string
	// MaxEntries optionally limits extracted file entries in portable mode. Zero
	// means unlimited.
	MaxEntries int
	// MaxUncompressedSize optionally limits total extracted bytes in portable
	// mode. Zero means unlimited.
	MaxUncompressedSize int64
}

type ArchiveTransferResult struct {
	Mode          ArchiveExtractMode
	ArchiveKey    string
	Extracted     []ArchiveExtractedObject
	ServerExtract bool
}

type ArchiveExtractedObject struct {
	Name        string
	Key         string
	Size        int64
	ContentType string
}

// TransferArchiveAndExtract moves an archive from S3 or HTTP(S) into a
// destination bucket and extracts it.
//
// Portable mode works with AWS S3, MinIO, RustFS, and generic S3-compatible
// targets by extracting in the application and uploading each file. MinIO
// Snowball mode streams the archive to MinIO with server-side auto-extract
// metadata for higher bulk-import throughput.
func TransferArchiveAndExtract(
	ctx context.Context,
	source *Client,
	destination *Client,
	opts ArchiveTransferOptions,
) (*ArchiveTransferResult, error) {
	opts = opts.normalized()
	if err := validateArchiveTransferOptions(source, destination, opts); err != nil {
		return nil, err
	}

	switch opts.Mode {
	case ArchiveExtractPortable:
		return transferArchivePortable(ctx, source, destination, opts)
	case ArchiveExtractMinIOSnowball:
		if err := transferArchiveMinIOSnowball(ctx, source, destination, opts); err != nil {
			return nil, err
		}
		return &ArchiveTransferResult{
			Mode:          opts.Mode,
			ArchiveKey:    opts.DestinationArchiveKey,
			Extracted:     []ArchiveExtractedObject{},
			ServerExtract: true,
		}, nil
	default:
		return nil, fmt.Errorf("%w: mode %q", ErrUnsupportedArchive, opts.Mode)
	}
}

func (opts ArchiveTransferOptions) normalized() ArchiveTransferOptions {
	if opts.Mode == "" {
		opts.Mode = ArchiveExtractPortable
	}
	return opts
}

func validateArchiveTransferOptions(source, destination *Client, opts ArchiveTransferOptions) error {
	if destination == nil {
		return errors.New("s3kit: destination client is nil")
	}
	if opts.SourceURL == "" && source == nil {
		return errors.New("s3kit: source client is nil")
	}
	if opts.SourceURL == "" && opts.SourceBucket == "" {
		return errors.New("s3kit: source bucket is required when source url is empty")
	}
	if opts.SourceURL == "" && opts.SourceKey == "" {
		return errors.New("s3kit: source key is required when source url is empty")
	}
	if opts.DestinationBucket == "" {
		return errors.New("s3kit: destination bucket is required")
	}
	if opts.Mode == ArchiveExtractMinIOSnowball && opts.DestinationArchiveKey == "" {
		return errors.New("s3kit: destination archive key is required for MinIO Snowball mode")
	}
	if opts.MaxEntries < 0 {
		return errors.New("s3kit: max entries must not be negative")
	}
	if opts.MaxUncompressedSize < 0 {
		return errors.New("s3kit: max uncompressed size must not be negative")
	}
	return nil
}

func transferArchivePortable(
	ctx context.Context,
	source *Client,
	destination *Client,
	opts ArchiveTransferOptions,
) (*ArchiveTransferResult, error) {
	tmpDir, err := os.MkdirTemp(opts.TempDir, "s3kit-archive-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	localPath := filepath.Join(tmpDir, "archive")
	if err := downloadArchiveSource(ctx, source, opts, localPath); err != nil {
		return nil, err
	}
	if opts.DestinationArchiveKey != "" {
		if err := uploadArchiveFile(ctx, destination, opts, localPath); err != nil {
			return nil, fmt.Errorf("upload destination archive: %w", err)
		}
	}

	format, err := detectArchiveFormat(opts)
	if err != nil {
		return nil, err
	}

	extracted, err := extractArchiveFile(ctx, destination, opts, localPath, format)
	if err != nil {
		return nil, err
	}
	return &ArchiveTransferResult{
		Mode:       opts.Mode,
		ArchiveKey: opts.DestinationArchiveKey,
		Extracted:  extracted,
	}, nil
}

func uploadArchiveFile(ctx context.Context, destination *Client, opts ArchiveTransferOptions, localPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	_, err = destination.uploadObject(
		ctx,
		opts.DestinationBucket,
		opts.DestinationArchiveKey,
		file,
		utils.DetectContentType(archiveSourceName(opts)),
		nil,
	)
	return err
}

func transferArchiveMinIOSnowball(
	ctx context.Context,
	source *Client,
	destination *Client,
	opts ArchiveTransferOptions,
) error {
	body, err := openArchiveSource(ctx, source, opts)
	if err != nil {
		return err
	}
	defer func() {
		_ = body.Close()
	}()

	contentType := opts.ContentType
	if contentType == "" {
		contentType = utils.DetectContentType(archiveSourceName(opts))
	}

	_, err = destination.uploadObject(
		ctx,
		opts.DestinationBucket,
		opts.DestinationArchiveKey,
		body,
		contentType,
		snowballAutoExtractMetadata(),
	)
	if err != nil {
		return fmt.Errorf("upload snowball auto-extract archive: %w", err)
	}
	return nil
}

func downloadArchiveSource(ctx context.Context, source *Client, opts ArchiveTransferOptions, localPath string) error {
	body, err := openArchiveSource(ctx, source, opts)
	if err != nil {
		return err
	}
	defer func() {
		_ = body.Close()
	}()

	file, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err := io.Copy(file, body); err != nil {
		return fmt.Errorf("write temporary archive: %w", err)
	}
	return nil
}

func openArchiveSource(ctx context.Context, source *Client, opts ArchiveTransferOptions) (io.ReadCloser, error) {
	if opts.SourceURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.SourceURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build source url request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get source url: %w", err)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("get source url failed: %s", resp.Status)
		}
		return resp.Body, nil
	}

	body, err := source.GetObject(ctx, opts.SourceBucket, opts.SourceKey)
	if err != nil {
		return nil, fmt.Errorf("get source archive: %w", err)
	}
	return body, nil
}

func extractArchiveFile(
	ctx context.Context,
	destination *Client,
	opts ArchiveTransferOptions,
	localPath string,
	format ArchiveFormat,
) ([]ArchiveExtractedObject, error) {
	switch format {
	case ArchiveFormatZip:
		return extractZipArchive(ctx, destination, opts, localPath)
	case ArchiveFormatTar, ArchiveFormatTarGz:
		return extractTarArchive(ctx, destination, opts, localPath, format == ArchiveFormatTarGz)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedArchive, format)
	}
}

func extractZipArchive(
	ctx context.Context,
	destination *Client,
	opts ArchiveTransferOptions,
	localPath string,
) ([]ArchiveExtractedObject, error) {
	reader, err := zip.OpenReader(localPath)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	extracted := []ArchiveExtractedObject{}
	tracker := archiveLimitTracker{maxEntries: opts.MaxEntries, maxSize: opts.MaxUncompressedSize}
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() || !entry.FileInfo().Mode().IsRegular() {
			continue
		}
		if entry.UncompressedSize64 > uint64(maxInt64) {
			return nil, fmt.Errorf("%w: %q is too large", ErrArchiveSizeLimit, entry.Name)
		}
		size := int64(entry.UncompressedSize64)
		if err := tracker.add(size); err != nil {
			return nil, err
		}
		object, err := uploadZipArchiveEntry(ctx, destination, opts, entry, size)
		if err != nil {
			return nil, err
		}
		extracted = append(extracted, object)
	}
	return extracted, nil
}

func uploadZipArchiveEntry(
	ctx context.Context,
	destination *Client,
	opts ArchiveTransferOptions,
	entry *zip.File,
	size int64,
) (ArchiveExtractedObject, error) {
	key, err := archiveEntryObjectKey(opts.ExtractPrefix, entry.Name)
	if err != nil {
		return ArchiveExtractedObject{}, err
	}
	body, err := entry.Open()
	if err != nil {
		return ArchiveExtractedObject{}, fmt.Errorf("open zip entry %q: %w", entry.Name, err)
	}
	defer func() {
		_ = body.Close()
	}()
	return uploadArchiveEntry(ctx, destination, opts.DestinationBucket, entry.Name, key, body, size)
}

func extractTarArchive(
	ctx context.Context,
	destination *Client,
	opts ArchiveTransferOptions,
	localPath string,
	gzipped bool,
) ([]ArchiveExtractedObject, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var tarSource io.Reader = file
	var gzipReader *gzip.Reader
	if gzipped {
		gzipReader, err = gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("open gzip: %w", err)
		}
		defer func() {
			_ = gzipReader.Close()
		}()
		tarSource = gzipReader
	}

	reader := tar.NewReader(tarSource)
	extracted := []ArchiveExtractedObject{}
	tracker := archiveLimitTracker{maxEntries: opts.MaxEntries, maxSize: opts.MaxUncompressedSize}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}
		if !header.FileInfo().Mode().IsRegular() {
			continue
		}
		if err := tracker.add(header.Size); err != nil {
			return nil, err
		}
		key, err := archiveEntryObjectKey(opts.ExtractPrefix, header.Name)
		if err != nil {
			return nil, err
		}
		object, err := uploadArchiveEntry(
			ctx,
			destination,
			opts.DestinationBucket,
			header.Name,
			key,
			reader,
			header.Size,
		)
		if err != nil {
			return nil, err
		}
		extracted = append(extracted, object)
	}
	return extracted, nil
}

func uploadArchiveEntry(
	ctx context.Context,
	destination *Client,
	bucket string,
	name string,
	key string,
	body io.Reader,
	size int64,
) (ArchiveExtractedObject, error) {
	contentType := utils.DetectContentType(name)
	if err := destination.PutObject(ctx, bucket, key, body, contentType); err != nil {
		return ArchiveExtractedObject{}, fmt.Errorf("upload archive entry %q: %w", name, err)
	}
	return ArchiveExtractedObject{
		Name:        name,
		Key:         key,
		Size:        size,
		ContentType: contentType,
	}, nil
}

type archiveLimitTracker struct {
	entries    int
	size       int64
	maxEntries int
	maxSize    int64
}

func (t *archiveLimitTracker) add(size int64) error {
	if size < 0 {
		return fmt.Errorf("%w: negative entry size", ErrArchiveSizeLimit)
	}
	if t.maxEntries > 0 && t.entries >= t.maxEntries {
		return fmt.Errorf("%w: max entries %d", ErrArchiveEntryLimit, t.maxEntries)
	}
	if size > maxInt64-t.size {
		return fmt.Errorf("%w: total size overflow", ErrArchiveSizeLimit)
	}
	t.size += size
	if t.maxSize > 0 && t.size > t.maxSize {
		return fmt.Errorf("%w: max size %d bytes", ErrArchiveSizeLimit, t.maxSize)
	}
	t.entries++
	return nil
}

func archiveEntryObjectKey(prefix, name string) (string, error) {
	cleanName, err := cleanArchiveEntryName(name)
	if err != nil {
		return "", err
	}
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return cleanName, nil
	}
	return prefix + "/" + cleanName, nil
}

func cleanArchiveEntryName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: empty name", ErrInvalidArchiveEntryName)
	}

	normalized := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("%w: absolute name %q", ErrInvalidArchiveEntryName, name)
	}
	if slices.Contains(strings.Split(normalized, "/"), "..") {
		return "", fmt.Errorf("%w: parent segment in %q", ErrInvalidArchiveEntryName, name)
	}

	cleanName := path.Clean(normalized)
	if cleanName == "." || cleanName == "/" {
		return "", fmt.Errorf("%w: empty name %q", ErrInvalidArchiveEntryName, name)
	}
	return cleanName, nil
}

func detectArchiveFormat(opts ArchiveTransferOptions) (ArchiveFormat, error) {
	if opts.Format != ArchiveFormatAuto {
		return opts.Format, nil
	}
	name := strings.ToLower(archiveSourceName(opts))
	switch {
	case strings.HasSuffix(name, ".zip"):
		return ArchiveFormatZip, nil
	case strings.HasSuffix(name, ".tar"):
		return ArchiveFormatTar, nil
	case strings.HasSuffix(name, ".tgz"), strings.HasSuffix(name, ".tar.gz"):
		return ArchiveFormatTarGz, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedArchive, name)
	}
}

func archiveSourceName(opts ArchiveTransferOptions) string {
	switch {
	case opts.SourceKey != "":
		return opts.SourceKey
	case opts.SourceURL != "":
		if i := strings.IndexByte(opts.SourceURL, '?'); i >= 0 {
			return opts.SourceURL[:i]
		}
		return opts.SourceURL
	default:
		return opts.DestinationArchiveKey
	}
}

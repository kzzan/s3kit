package s3kit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/kzzan/s3kit/utils"
)

var (
	// ErrInvalidVersionNumber 表示传入的对象版本号不是正整数。
	ErrInvalidVersionNumber = errors.New("s3kit: version number must be greater than zero")
	// ErrObjectVersionNotFound 表示请求的对象历史版本不存在。
	ErrObjectVersionNotFound = errors.New("s3kit: object version not found")
)

// Client wraps the S3 and transfer manager clients used by the package.
type Client struct {
	s3Client  *s3.Client
	transfer  *transfermanager.Client
	presigner *s3.PresignClient
}

// ObjectVersion 描述对象的一个可读取历史版本。
//
// VersionNumber 使用从 1 开始的序号：1 表示最新对象版本，2 表示上一个对象版本，以此类推。
type ObjectVersion struct {
	VersionNumber int
	VersionID     string
	IsLatest      bool
	LastModified  time.Time
	Size          int64
	ETag          string
}

// ObjectVersionIdentifier 标识一个指定 VersionID 的对象历史版本。
type ObjectVersionIdentifier struct {
	Key       string
	VersionID string
}

// ObjectVersionNumberIdentifier 标识一个指定版本号的对象历史版本。
type ObjectVersionNumberIdentifier struct {
	Key           string
	VersionNumber int
}

// New builds a Client with a background context.
func New(cfg Config) (*Client, error) {
	return NewContext(context.Background(), cfg)
}

// NewContext builds a Client using the provided context while loading AWS
// configuration.
func NewContext(ctx context.Context, cfg Config) (*Client, error) {
	cfg = cfg.normalized()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	loadOptions := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}
	if cfg.hasStaticCredentials() {
		loadOptions = append(loadOptions, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID,
				cfg.SecretAccessKey,
				cfg.SessionToken,
			),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint == "" {
			return
		}
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})

	return &Client{
		s3Client:  s3Client,
		transfer:  transfermanager.New(s3Client),
		presigner: s3.NewPresignClient(s3Client),
	}, nil
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound"
}

// CreateBucket creates a bucket.
func (c *Client) CreateBucket(ctx context.Context, name string) error {
	_, err := c.s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

// DeleteBucket deletes a bucket.
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	_, err := c.s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

// BucketExists reports whether a bucket exists.
func (c *Client) BucketExists(ctx context.Context, name string) (bool, error) {
	_, err := c.s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(name),
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// WaitBucketExists blocks until the bucket exists or the timeout expires.
func (c *Client) WaitBucketExists(ctx context.Context, name string, timeout time.Duration) error {
	waiter := s3.NewBucketExistsWaiter(c.s3Client)
	return waiter.Wait(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(name),
	}, timeout)
}

// ListBuckets lists all visible bucket names.
func (c *Client) ListBuckets(ctx context.Context) ([]string, error) {
	resp, err := c.s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	buckets := make([]string, 0, len(resp.Buckets))
	for _, b := range resp.Buckets {
		if b.Name != nil {
			buckets = append(buckets, *b.Name)
		}
	}
	return buckets, nil
}

// PutObject uploads data from body to the given bucket and key.
func (c *Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, contentType string) error {
	_, err := c.putObject(ctx, bucket, key, body, contentType, nil)
	return err
}

// PutObjectSnowballAutoExtract uploads an archive and asks MinIO to extract it
// server-side by sending x-amz-meta-snowball-auto-extract: true.
//
// This is a MinIO-specific extension for .tar, .tgz, and .zip bulk imports.
// Other S3-compatible services may store the metadata without extracting.
func (c *Client) PutObjectSnowballAutoExtract(
	ctx context.Context,
	bucket string,
	key string,
	body io.Reader,
	contentType string,
) error {
	_, err := c.putObject(ctx, bucket, key, body, contentType, snowballAutoExtractMetadata())
	return err
}

// PutObjectVersion 上传对象并在桶启用版本控制时返回新对象的 VersionID。
func (c *Client) PutObjectVersion(
	ctx context.Context,
	bucket string,
	key string,
	body io.Reader,
	contentType string,
) (string, error) {
	output, err := c.putObject(ctx, bucket, key, body, contentType, nil)
	if err != nil {
		return "", err
	}
	return aws.ToString(output.VersionId), nil
}

func (c *Client) putObject(
	ctx context.Context,
	bucket string,
	key string,
	body io.Reader,
	contentType string,
	metadata map[string]string,
) (*s3.PutObjectOutput, error) {
	return c.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
		Metadata:    metadata,
	})
}

// PutObjectBytes uploads an in-memory byte slice.
func (c *Client) PutObjectBytes(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	return c.PutObject(ctx, bucket, key, bytes.NewReader(data), contentType)
}

// PutObjectBytesSnowballAutoExtract uploads an in-memory archive and asks MinIO
// to extract it server-side.
func (c *Client) PutObjectBytesSnowballAutoExtract(
	ctx context.Context,
	bucket string,
	key string,
	data []byte,
	contentType string,
) error {
	return c.PutObjectSnowballAutoExtract(ctx, bucket, key, bytes.NewReader(data), contentType)
}

// PutObjectBytesVersion 上传内存字节切片并在桶启用版本控制时返回新对象的 VersionID。
func (c *Client) PutObjectBytesVersion(
	ctx context.Context,
	bucket string,
	key string,
	data []byte,
	contentType string,
) (string, error) {
	return c.PutObjectVersion(ctx, bucket, key, bytes.NewReader(data), contentType)
}

// GetObject fetches an object body. The caller must close the returned reader.
func (c *Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return c.getObject(ctx, bucket, key, "")
}

// GetObjectVersion 按 VersionID 读取对象的指定历史版本。
// 调用方必须关闭返回的 reader。
func (c *Client) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, error) {
	return c.getObject(ctx, bucket, key, versionID)
}

// GetObjectVersionByNumber 按从 1 开始的版本号读取对象历史版本。
// 版本号 1 表示最新对象版本；调用方必须关闭返回的 reader。
func (c *Client) GetObjectVersionByNumber(
	ctx context.Context,
	bucket string,
	key string,
	versionNumber int,
) (io.ReadCloser, error) {
	versionID, err := c.ObjectVersionID(ctx, bucket, key, versionNumber)
	if err != nil {
		return nil, err
	}
	return c.GetObjectVersion(ctx, bucket, key, versionID)
}

// GetObjectBytes fetches an object into memory.
func (c *Client) GetObjectBytes(ctx context.Context, bucket, key string) ([]byte, error) {
	return c.getObjectBytes(ctx, bucket, key, "")
}

// GetObjectVersionBytes 按 VersionID 将对象历史版本读取到内存。
func (c *Client) GetObjectVersionBytes(ctx context.Context, bucket, key, versionID string) ([]byte, error) {
	return c.getObjectBytes(ctx, bucket, key, versionID)
}

// GetObjectVersionBytesByNumber 按从 1 开始的版本号将对象历史版本读取到内存。
// 版本号 1 表示最新对象版本。
func (c *Client) GetObjectVersionBytesByNumber(
	ctx context.Context,
	bucket string,
	key string,
	versionNumber int,
) ([]byte, error) {
	versionID, err := c.ObjectVersionID(ctx, bucket, key, versionNumber)
	if err != nil {
		return nil, err
	}
	return c.GetObjectVersionBytes(ctx, bucket, key, versionID)
}

// DeleteObject deletes a single object.
func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.s3Client.DeleteObject(ctx, deleteObjectInput(bucket, key, ""))
	return err
}

// DeleteObjectVersion 按 VersionID 永久删除指定对象历史版本。
func (c *Client) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	_, err := c.s3Client.DeleteObject(ctx, deleteObjectInput(bucket, key, versionID))
	return err
}

// DeleteObjectVersionByNumber 按从 1 开始的版本号永久删除指定对象历史版本。
func (c *Client) DeleteObjectVersionByNumber(
	ctx context.Context,
	bucket string,
	key string,
	versionNumber int,
) error {
	versionID, err := c.ObjectVersionID(ctx, bucket, key, versionNumber)
	if err != nil {
		return err
	}
	return c.DeleteObjectVersion(ctx, bucket, key, versionID)
}

func deleteObjectInput(bucket, key, versionID string) *s3.DeleteObjectInput {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	return input
}

// ObjectExists reports whether an object exists.
func (c *Client) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	return c.objectExists(ctx, bucket, key, "")
}

// ObjectVersionExists 判断指定 VersionID 的对象历史版本是否存在。
func (c *Client) ObjectVersionExists(ctx context.Context, bucket, key, versionID string) (bool, error) {
	return c.objectExists(ctx, bucket, key, versionID)
}

// ObjectVersionExistsByNumber 判断指定版本号的对象历史版本是否存在。
func (c *Client) ObjectVersionExistsByNumber(
	ctx context.Context,
	bucket string,
	key string,
	versionNumber int,
) (bool, error) {
	versionID, err := c.ObjectVersionID(ctx, bucket, key, versionNumber)
	if err != nil {
		return false, err
	}
	return c.ObjectVersionExists(ctx, bucket, key, versionID)
}

func (c *Client) objectExists(ctx context.Context, bucket, key, versionID string) (bool, error) {
	_, err := c.s3Client.HeadObject(ctx, headObjectInput(bucket, key, versionID))
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func headObjectInput(bucket, key, versionID string) *s3.HeadObjectInput {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	return input
}

func (c *Client) getObject(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, error) {
	output, err := c.s3Client.GetObject(ctx, objectInput(bucket, key, versionID))
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (c *Client) getObjectBytes(ctx context.Context, bucket, key, versionID string) ([]byte, error) {
	obj, err := c.getObject(ctx, bucket, key, versionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = obj.Close()
	}()
	return io.ReadAll(obj)
}

func objectInput(bucket, key, versionID string) *s3.GetObjectInput {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	return input
}

// ListObjectVersions 列出单个对象 key 的所有可读取历史版本。
//
// 返回的 VersionNumber 从 1 开始：1 表示最新对象版本，2 表示上一个对象版本。
// 删除标记不可作为文件内容读取，因此不会出现在返回结果里。
func (c *Client) ListObjectVersions(ctx context.Context, bucket, key string) ([]ObjectVersion, error) {
	versions := []ObjectVersion{}
	paginator := s3.NewListObjectVersionsPaginator(c.s3Client, &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(key),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		versions = appendMatchingObjectVersions(versions, key, page.Versions)
	}
	return versions, nil
}

// ObjectVersionID 将从 1 开始的版本号解析为 S3 的 VersionID。
// 版本号 1 表示最新对象版本。
func (c *Client) ObjectVersionID(ctx context.Context, bucket, key string, versionNumber int) (string, error) {
	if err := validateVersionNumber(versionNumber); err != nil {
		return "", err
	}

	versions, err := c.ListObjectVersions(ctx, bucket, key)
	if err != nil {
		return "", err
	}
	if versionNumber > len(versions) {
		return "", fmt.Errorf("%w: key %q version number %d", ErrObjectVersionNotFound, key, versionNumber)
	}
	return versions[versionNumber-1].VersionID, nil
}

func validateVersionNumber(versionNumber int) error {
	if versionNumber <= 0 {
		return ErrInvalidVersionNumber
	}
	return nil
}

func appendMatchingObjectVersions(
	versions []ObjectVersion,
	key string,
	s3Versions []types.ObjectVersion,
) []ObjectVersion {
	for _, s3Version := range s3Versions {
		if aws.ToString(s3Version.Key) != key {
			continue
		}
		versions = append(versions, objectVersion(len(versions)+1, s3Version))
	}
	return versions
}

func objectVersion(versionNumber int, s3Version types.ObjectVersion) ObjectVersion {
	var lastModified time.Time
	if s3Version.LastModified != nil {
		lastModified = *s3Version.LastModified
	}

	return ObjectVersion{
		VersionNumber: versionNumber,
		VersionID:     aws.ToString(s3Version.VersionId),
		IsLatest:      aws.ToBool(s3Version.IsLatest),
		LastModified:  lastModified,
		Size:          aws.ToInt64(s3Version.Size),
		ETag:          aws.ToString(s3Version.ETag),
	}
}

// ListObjects lists all object keys under prefix.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(c.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
	}
	return keys, nil
}

const deleteChunkSize = 1000

// DeleteObjects deletes objects in batches that respect the S3 1000-key limit.
func (c *Client) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return c.deleteObjects(ctx, bucket, objectIdentifiers(keys))
}

// DeleteObjectVersions 按 VersionID 批量删除对象历史版本，并自动按 S3 1000 个对象限制分批。
func (c *Client) DeleteObjectVersions(
	ctx context.Context,
	bucket string,
	versions []ObjectVersionIdentifier,
) error {
	return c.deleteObjects(ctx, bucket, objectVersionIdentifiers(versions))
}

// DeleteObjectVersionsByNumber 按版本号批量删除对象历史版本，并自动按 S3 1000 个对象限制分批。
func (c *Client) DeleteObjectVersionsByNumber(
	ctx context.Context,
	bucket string,
	versions []ObjectVersionNumberIdentifier,
) error {
	identifiers := make([]ObjectVersionIdentifier, 0, len(versions))
	for _, version := range versions {
		versionID, err := c.ObjectVersionID(ctx, bucket, version.Key, version.VersionNumber)
		if err != nil {
			return err
		}
		identifiers = append(identifiers, ObjectVersionIdentifier{
			Key:       version.Key,
			VersionID: versionID,
		})
	}
	return c.DeleteObjectVersions(ctx, bucket, identifiers)
}

// DeleteAllObjectVersions 永久删除单个对象 key 的所有历史版本和删除标记。
func (c *Client) DeleteAllObjectVersions(ctx context.Context, bucket, key string) error {
	versions, err := c.listObjectVersionIdentifiers(ctx, bucket, key, true)
	if err != nil {
		return err
	}
	return c.DeleteObjectVersions(ctx, bucket, versions)
}

// EmptyBucketVersions 永久删除版本桶里的所有对象历史版本和删除标记。
func (c *Client) EmptyBucketVersions(ctx context.Context, bucket string) error {
	versions, err := c.listObjectVersionIdentifiers(ctx, bucket, "", false)
	if err != nil {
		return err
	}
	return c.DeleteObjectVersions(ctx, bucket, versions)
}

func (c *Client) deleteObjects(ctx context.Context, bucket string, identifiers []types.ObjectIdentifier) error {
	for i := 0; i < len(identifiers); i += deleteChunkSize {
		end := min(i+deleteChunkSize, len(identifiers))
		output, err := c.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{Objects: identifiers[i:end]},
		})
		if err != nil {
			return err
		}
		if err := joinDeleteErrors(output.Errors); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) listObjectVersionIdentifiers(
	ctx context.Context,
	bucket string,
	prefix string,
	exactKey bool,
) ([]ObjectVersionIdentifier, error) {
	versions := []ObjectVersionIdentifier{}
	paginator := s3.NewListObjectVersionsPaginator(c.s3Client, &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		versions = appendObjectVersionIdentifiers(versions, page.Versions, exactKey, prefix)
		versions = appendDeleteMarkerIdentifiers(versions, page.DeleteMarkers, exactKey, prefix)
	}
	return versions, nil
}

func appendObjectVersionIdentifiers(
	identifiers []ObjectVersionIdentifier,
	versions []types.ObjectVersion,
	exactKey bool,
	key string,
) []ObjectVersionIdentifier {
	for _, version := range versions {
		if exactKey && aws.ToString(version.Key) != key {
			continue
		}
		identifiers = append(identifiers, ObjectVersionIdentifier{
			Key:       aws.ToString(version.Key),
			VersionID: aws.ToString(version.VersionId),
		})
	}
	return identifiers
}

func appendDeleteMarkerIdentifiers(
	identifiers []ObjectVersionIdentifier,
	deleteMarkers []types.DeleteMarkerEntry,
	exactKey bool,
	key string,
) []ObjectVersionIdentifier {
	for _, deleteMarker := range deleteMarkers {
		if exactKey && aws.ToString(deleteMarker.Key) != key {
			continue
		}
		identifiers = append(identifiers, ObjectVersionIdentifier{
			Key:       aws.ToString(deleteMarker.Key),
			VersionID: aws.ToString(deleteMarker.VersionId),
		})
	}
	return identifiers
}

func objectIdentifiers(keys []string) []types.ObjectIdentifier {
	identifiers := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		identifiers[i] = types.ObjectIdentifier{Key: aws.String(key)}
	}
	return identifiers
}

func objectVersionIdentifiers(versions []ObjectVersionIdentifier) []types.ObjectIdentifier {
	identifiers := make([]types.ObjectIdentifier, len(versions))
	for i, version := range versions {
		identifiers[i] = types.ObjectIdentifier{
			Key:       aws.String(version.Key),
			VersionId: aws.String(version.VersionID),
		}
	}
	return identifiers
}

func joinDeleteErrors(deleteErrs []types.Error) error {
	if len(deleteErrs) == 0 {
		return nil
	}

	errs := make([]error, 0, len(deleteErrs))
	for _, deleteErr := range deleteErrs {
		key := aws.ToString(deleteErr.Key)
		versionID := aws.ToString(deleteErr.VersionId)
		code := aws.ToString(deleteErr.Code)
		message := aws.ToString(deleteErr.Message)

		switch {
		case key != "" && versionID != "" && code != "" && message != "":
			errs = append(errs, fmt.Errorf(
				"delete %q version %q failed with %s: %s",
				key,
				versionID,
				code,
				message,
			))
		case key != "" && versionID != "" && message != "":
			errs = append(errs, fmt.Errorf("delete %q version %q failed: %s", key, versionID, message))
		case key != "" && code != "" && message != "":
			errs = append(errs, fmt.Errorf("delete %q failed with %s: %s", key, code, message))
		case key != "" && message != "":
			errs = append(errs, fmt.Errorf("delete %q failed: %s", key, message))
		case message != "":
			errs = append(errs, errors.New(message))
		default:
			errs = append(errs, errors.New("s3kit: delete objects returned an unknown error"))
		}
	}

	return errors.Join(errs...)
}

// UploadFile uploads a local file and infers its content type from the file
// extension.
func (c *Client) UploadFile(ctx context.Context, bucket, key, localPath string) error {
	_, err := c.uploadFile(ctx, bucket, key, localPath, nil)
	return err
}

// UploadFileSnowballAutoExtract uploads a .tar, .tgz, or .zip archive and asks
// MinIO to extract it server-side by sending
// x-amz-meta-snowball-auto-extract: true.
//
// This optimizes bulk imports of many small files into MinIO. It is not a
// portable S3 standard; non-MinIO services may store the archive as a normal
// object with user metadata and perform no extraction.
func (c *Client) UploadFileSnowballAutoExtract(ctx context.Context, bucket, key, localPath string) error {
	_, err := c.uploadFile(ctx, bucket, key, localPath, snowballAutoExtractMetadata())
	return err
}

// UploadFileVersion 上传本地文件并在桶启用版本控制时返回新对象的 VersionID。
func (c *Client) UploadFileVersion(ctx context.Context, bucket, key, localPath string) (string, error) {
	output, err := c.uploadFile(ctx, bucket, key, localPath, nil)
	if err != nil {
		return "", err
	}
	return aws.ToString(output.VersionID), nil
}

func (c *Client) uploadFile(
	ctx context.Context,
	bucket string,
	key string,
	localPath string,
	metadata map[string]string,
) (*transfermanager.UploadObjectOutput, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	return c.uploadObject(ctx, bucket, key, file, utils.DetectContentType(localPath), metadata)
}

func (c *Client) uploadObject(
	ctx context.Context,
	bucket string,
	key string,
	body io.Reader,
	contentType string,
	metadata map[string]string,
) (*transfermanager.UploadObjectOutput, error) {
	return c.transfer.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
		Metadata:    metadata,
	})
}

// DownloadFile downloads an object atomically by writing to a temporary file and
// renaming it on success.
func (c *Client) DownloadFile(ctx context.Context, bucket, key, localPath string) error {
	return c.downloadFile(ctx, bucket, key, localPath, "")
}

// DownloadFileVersion 按 VersionID 原子下载对象历史版本，成功后再重命名临时文件。
func (c *Client) DownloadFileVersion(ctx context.Context, bucket, key, localPath, versionID string) error {
	return c.downloadFile(ctx, bucket, key, localPath, versionID)
}

// DownloadFileVersionByNumber 按从 1 开始的版本号原子下载对象历史版本。
// 版本号 1 表示最新对象版本。
func (c *Client) DownloadFileVersionByNumber(
	ctx context.Context,
	bucket string,
	key string,
	localPath string,
	versionNumber int,
) error {
	versionID, err := c.ObjectVersionID(ctx, bucket, key, versionNumber)
	if err != nil {
		return err
	}
	return c.DownloadFileVersion(ctx, bucket, key, localPath, versionID)
}

func (c *Client) downloadFile(ctx context.Context, bucket, key, localPath, versionID string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".s3kit-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	_, err = c.transfer.DownloadObject(ctx, downloadObjectInput(bucket, key, tmp, versionID))
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func downloadObjectInput(
	bucket string,
	key string,
	writer io.WriterAt,
	versionID string,
) *transfermanager.DownloadObjectInput {
	input := &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		WriterAt: writer,
	}
	if versionID != "" {
		input.VersionID = aws.String(versionID)
	}
	return input
}

// CopyObject copies an object to a new bucket/key pair.
func (c *Client) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	_, err := c.copyObject(ctx, srcBucket, srcKey, "", dstBucket, dstKey)
	return err
}

// CopyObjectVersion 按 VersionID 复制源对象历史版本，并在目标桶启用版本控制时返回新 VersionID。
func (c *Client) CopyObjectVersion(
	ctx context.Context,
	srcBucket string,
	srcKey string,
	srcVersionID string,
	dstBucket string,
	dstKey string,
) (string, error) {
	output, err := c.copyObject(ctx, srcBucket, srcKey, srcVersionID, dstBucket, dstKey)
	if err != nil {
		return "", err
	}
	return aws.ToString(output.VersionId), nil
}

// CopyObjectVersionByNumber 按从 1 开始的版本号复制源对象历史版本。
func (c *Client) CopyObjectVersionByNumber(
	ctx context.Context,
	srcBucket string,
	srcKey string,
	versionNumber int,
	dstBucket string,
	dstKey string,
) (string, error) {
	srcVersionID, err := c.ObjectVersionID(ctx, srcBucket, srcKey, versionNumber)
	if err != nil {
		return "", err
	}
	return c.CopyObjectVersion(ctx, srcBucket, srcKey, srcVersionID, dstBucket, dstKey)
}

func (c *Client) copyObject(
	ctx context.Context,
	srcBucket string,
	srcKey string,
	srcVersionID string,
	dstBucket string,
	dstKey string,
) (*s3.CopyObjectOutput, error) {
	return c.s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		CopySource: aws.String(copySource(srcBucket, srcKey, srcVersionID)),
		Key:        aws.String(dstKey),
	})
}

func copySource(bucket, key, versionID string) string {
	source := url.PathEscape(fmt.Sprintf("%s/%s", bucket, key))
	if versionID != "" {
		source += "?versionId=" + url.QueryEscape(versionID)
	}
	return source
}

// MoveObject copies an object and removes the source key.
func (c *Client) MoveObject(ctx context.Context, bucket, srcKey, dstKey string) error {
	if err := c.CopyObject(ctx, bucket, srcKey, bucket, dstKey); err != nil {
		return err
	}
	return c.DeleteObject(ctx, bucket, srcKey)
}

// MoveObjectVersion 按 VersionID 将源对象历史版本移动到新 key。
// 该方法会先复制该历史版本，再永久删除源 VersionID；目标桶启用版本控制时返回新 VersionID。
func (c *Client) MoveObjectVersion(
	ctx context.Context,
	bucket string,
	srcKey string,
	srcVersionID string,
	dstKey string,
) (string, error) {
	dstVersionID, err := c.CopyObjectVersion(ctx, bucket, srcKey, srcVersionID, bucket, dstKey)
	if err != nil {
		return "", err
	}
	if err := c.DeleteObjectVersion(ctx, bucket, srcKey, srcVersionID); err != nil {
		return "", err
	}
	return dstVersionID, nil
}

// MoveObjectVersionByNumber 按从 1 开始的版本号将源对象历史版本移动到新 key。
func (c *Client) MoveObjectVersionByNumber(
	ctx context.Context,
	bucket string,
	srcKey string,
	versionNumber int,
	dstKey string,
) (string, error) {
	srcVersionID, err := c.ObjectVersionID(ctx, bucket, srcKey, versionNumber)
	if err != nil {
		return "", err
	}
	return c.MoveObjectVersion(ctx, bucket, srcKey, srcVersionID, dstKey)
}

// PresignGetObject creates a pre-signed GET URL.
func (c *Client) PresignGetObject(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	return c.presignGetObject(ctx, bucket, key, "", expiry)
}

// PresignGetObjectVersion 按 VersionID 创建对象历史版本的预签名 GET URL。
func (c *Client) PresignGetObjectVersion(
	ctx context.Context,
	bucket string,
	key string,
	versionID string,
	expiry time.Duration,
) (string, error) {
	return c.presignGetObject(ctx, bucket, key, versionID, expiry)
}

// PresignGetObjectVersionByNumber 按从 1 开始的版本号创建对象历史版本的预签名 GET URL。
func (c *Client) PresignGetObjectVersionByNumber(
	ctx context.Context,
	bucket string,
	key string,
	versionNumber int,
	expiry time.Duration,
) (string, error) {
	versionID, err := c.ObjectVersionID(ctx, bucket, key, versionNumber)
	if err != nil {
		return "", err
	}
	return c.PresignGetObjectVersion(ctx, bucket, key, versionID, expiry)
}

func (c *Client) presignGetObject(
	ctx context.Context,
	bucket string,
	key string,
	versionID string,
	expiry time.Duration,
) (string, error) {
	req, err := c.presigner.PresignGetObject(ctx, objectInput(bucket, key, versionID), func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// PresignPutObject creates a pre-signed PUT URL.
func (c *Client) PresignPutObject(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	req, err := c.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// EmptyBucket deletes every object in a bucket.
func (c *Client) EmptyBucket(ctx context.Context, bucket string) error {
	objects, err := c.ListObjects(ctx, bucket, "")
	if err != nil {
		return err
	}
	return c.DeleteObjects(ctx, bucket, objects)
}

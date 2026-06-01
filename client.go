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

// Client wraps the S3 and transfer manager clients used by the package.
type Client struct {
	s3Client  *s3.Client
	transfer  *transfermanager.Client
	presigner *s3.PresignClient
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
	_, err := c.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	return err
}

// PutObjectBytes uploads an in-memory byte slice.
func (c *Client) PutObjectBytes(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	return c.PutObject(ctx, bucket, key, bytes.NewReader(data), contentType)
}

// GetObject fetches an object body. The caller must close the returned reader.
func (c *Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	output, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

// GetObjectBytes fetches an object into memory.
func (c *Client) GetObjectBytes(ctx context.Context, bucket, key string) ([]byte, error) {
	obj, err := c.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// DeleteObject deletes a single object.
func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// ObjectExists reports whether an object exists.
func (c *Client) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
	for i := 0; i < len(keys); i += deleteChunkSize {
		end := min(i+deleteChunkSize, len(keys))
		identifiers := make([]types.ObjectIdentifier, end-i)
		for j, key := range keys[i:end] {
			identifiers[j] = types.ObjectIdentifier{Key: aws.String(key)}
		}
		output, err := c.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{Objects: identifiers},
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

func joinDeleteErrors(deleteErrs []types.Error) error {
	if len(deleteErrs) == 0 {
		return nil
	}

	errs := make([]error, 0, len(deleteErrs))
	for _, deleteErr := range deleteErrs {
		key := aws.ToString(deleteErr.Key)
		code := aws.ToString(deleteErr.Code)
		message := aws.ToString(deleteErr.Message)

		switch {
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
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = c.transfer.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(utils.DetectContentType(localPath)),
	})
	return err
}

// DownloadFile downloads an object atomically by writing to a temporary file and
// renaming it on success.
func (c *Client) DownloadFile(ctx context.Context, bucket, key, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".s3kit-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	_, err = c.transfer.DownloadObject(ctx, &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		WriterAt: tmp,
	})
	if err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// CopyObject copies an object to a new bucket/key pair.
func (c *Client) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	_, err := c.s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		CopySource: aws.String(copySource(srcBucket, srcKey)),
		Key:        aws.String(dstKey),
	})
	return err
}

func copySource(bucket, key string) string {
	return url.PathEscape(fmt.Sprintf("%s/%s", bucket, key))
}

// MoveObject copies an object and removes the source key.
func (c *Client) MoveObject(ctx context.Context, bucket, srcKey, dstKey string) error {
	if err := c.CopyObject(ctx, bucket, srcKey, bucket, dstKey); err != nil {
		return err
	}
	return c.DeleteObject(ctx, bucket, srcKey)
}

// PresignGetObject creates a pre-signed GET URL.
func (c *Client) PresignGetObject(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	req, err := c.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
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

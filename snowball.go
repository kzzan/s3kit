package s3kit

import "context"

const snowballAutoExtractMetadataKey = "snowball-auto-extract"

// SnowballAutoExtractTransferOptions configures TransferSnowballAutoExtract.
type SnowballAutoExtractTransferOptions struct {
	// SourceBucket and SourceKey identify the remote archive object to read when
	// SourceURL is empty.
	SourceBucket string
	SourceKey    string
	// SourceURL is an optional HTTP(S) or presigned S3 URL. When set,
	// SourceBucket is not required and source may be nil.
	SourceURL string
	// DestinationBucket and DestinationKey identify where the archive is
	// uploaded on the MinIO target.
	DestinationBucket string
	DestinationKey    string
	// ContentType optionally overrides the uploaded archive content type. Leave
	// empty to infer from SourceKey or SourceURL.
	ContentType string
}

// TransferSnowballAutoExtract streams a remote archive from source to
// destination and asks the destination MinIO server to extract it.
//
// The source can be any S3-compatible endpoint that supports GetObject. The
// destination must be MinIO with Snowball Auto-Extract enabled, otherwise the
// archive is stored as a normal object with user metadata.
func TransferSnowballAutoExtract(
	ctx context.Context,
	source *Client,
	destination *Client,
	opts SnowballAutoExtractTransferOptions,
) error {
	archiveOpts := ArchiveTransferOptions{
		SourceBucket:          opts.SourceBucket,
		SourceKey:             opts.SourceKey,
		SourceURL:             opts.SourceURL,
		DestinationBucket:     opts.DestinationBucket,
		DestinationArchiveKey: opts.DestinationKey,
		Mode:                  ArchiveExtractMinIOSnowball,
		ContentType:           opts.ContentType,
	}
	_, err := TransferArchiveAndExtract(ctx, source, destination, archiveOpts)
	return err
}

func snowballAutoExtractMetadata() map[string]string {
	return map[string]string{
		snowballAutoExtractMetadataKey: "true",
	}
}

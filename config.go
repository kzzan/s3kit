package s3kit

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
)

const defaultRegion = "us-east-1"

var errStaticCredentialsIncomplete = errors.New("s3kit: access key id and secret access key must be provided together")

// Config configures a Client.
//
// If AccessKeyID and SecretAccessKey are left empty, s3kit uses the AWS SDK v2
// default credential chain. When Endpoint is set, s3kit assumes a generic
// S3-compatible service and enables path-style addressing automatically.
type Config struct {
	// Endpoint is the base URL of the S3-compatible service. Leave it empty to
	// use the default AWS S3 endpoint resolution for the selected Region.
	Endpoint string
	// AccessKeyID is the static access key to use. Leave it empty to use the
	// default AWS credential chain.
	AccessKeyID string
	// SecretAccessKey is the static secret key to use. It must be paired with
	// AccessKeyID when set.
	SecretAccessKey string
	// SessionToken is an optional session token for temporary static
	// credentials.
	SessionToken string
	// Region is the AWS region or S3-compatible region name. It defaults to
	// us-east-1 when omitted.
	Region string
}

func (cfg Config) normalized() Config {
	cfg.Region = cmp.Or(cfg.Region, defaultRegion)
	return cfg
}

func (cfg Config) validate() error {
	if (cfg.AccessKeyID == "") != (cfg.SecretAccessKey == "") {
		return errStaticCredentialsIncomplete
	}
	if cfg.SessionToken != "" && !cfg.hasStaticCredentials() {
		return errors.New("s3kit: session token requires access key id and secret access key")
	}
	if cfg.Endpoint != "" {
		if _, err := url.ParseRequestURI(cfg.Endpoint); err != nil {
			return fmt.Errorf("s3kit: invalid endpoint: %w", err)
		}
	}
	return nil
}

func (cfg Config) hasStaticCredentials() bool {
	return cfg.AccessKeyID != "" && cfg.SecretAccessKey != ""
}

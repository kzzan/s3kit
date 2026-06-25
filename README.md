# s3kit

[![Go Reference](https://pkg.go.dev/badge/github.com/kzzan/s3kit.svg)](https://pkg.go.dev/github.com/kzzan/s3kit)
[![Go Report Card](https://goreportcard.com/badge/github.com/kzzan/s3kit)](https://goreportcard.com/report/github.com/kzzan/s3kit)
[![CI](https://github.com/kzzan/s3kit/actions/workflows/ci.yml/badge.svg)](https://github.com/kzzan/s3kit/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kzzan/s3kit)](https://github.com/kzzan/s3kit/releases)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)

A simple, idiomatic Go SDK for S3-compatible object storage. Works with AWS S3, MinIO, RustFS, and any S3-compatible service.

## Features

- Bucket operations: create, delete, list, check existence, empty
- Object operations: put, get, delete, copy, move, list
- Historical object operations by version ID or 1-based version number
- `ListObjects` auto-paginates — returns all objects regardless of count
- `DeleteObjects` auto-batches — handles any number of keys (1000/request limit handled internally)
- File upload/download with automatic content-type detection (100+ extensions)
- `DownloadFile` is atomic — no partial files left on failure
- Presigned URLs for GET and PUT
- Multipart upload/download via `aws-sdk-go-v2/feature/s3/transfermanager`
- Minimal API surface — one struct, zero global state

## Installation

```bash
go get github.com/kzzan/s3kit@latest
```

Requires Go 1.26+.

## Quick Start

**AWS S3 using the default credential chain**

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/kzzan/s3kit"
)

func main() {
    client, err := s3kit.New(s3kit.Config{
        Region:          "us-east-1",
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    if err := client.PutObjectBytes(ctx, "my-bucket", "hello.txt", []byte("hello, world"), "text/plain"); err != nil {
        log.Fatal(err)
    }

    data, err := client.GetObjectBytes(ctx, "my-bucket", "hello.txt")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(string(data))
}
```

`s3kit.New` uses the AWS SDK v2 default credential chain when `AccessKeyID` and `SecretAccessKey` are omitted.

**S3-compatible storage with explicit credentials**

```go
client, err := s3kit.New(s3kit.Config{
    Endpoint:        "http://localhost:9000",
    AccessKeyID:     "minioadmin",
    SecretAccessKey: "minioadmin",
})
```

## Configuration

| Field | Description | Default |
|---|---|---|
| `Endpoint` | S3-compatible endpoint URL. Leave empty for AWS default endpoint resolution. | optional |
| `AccessKeyID` | Access key / username. Leave empty to use the AWS SDK default credential chain. | optional |
| `SecretAccessKey` | Secret key / password paired with `AccessKeyID`. | optional |
| `SessionToken` | Optional session token for temporary credentials. | empty |
| `Region` | Region name | `us-east-1` |

**MinIO**

```go
client, err := s3kit.New(s3kit.Config{
    Endpoint:        "http://localhost:9000",
    AccessKeyID:     "minioadmin",
    SecretAccessKey: "minioadmin",
    SessionToken:    "",
})
```

**RustFS**

```go
client, err := s3kit.New(s3kit.Config{
    Endpoint:        "http://localhost:9001",
    AccessKeyID:     "rustfsadmin",
    SecretAccessKey: "rustfsadmin",
})
```

## API Reference

### Bucket Operations

```go
err := client.CreateBucket(ctx, "my-bucket")
err := client.DeleteBucket(ctx, "my-bucket")

exists, err := client.BucketExists(ctx, "my-bucket")
err  := client.WaitBucketExists(ctx, "my-bucket", 30*time.Second)

buckets, err := client.ListBuckets(ctx)
err          := client.EmptyBucket(ctx, "my-bucket")
err          := client.EmptyBucketVersions(ctx, "my-bucket")
```

### Object Operations

```go
// Write
err := client.PutObject(ctx, "bucket", "key", reader, "application/octet-stream")
err := client.PutObjectBytes(ctx, "bucket", "key", data, "text/plain")
versionID, err := client.PutObjectVersion(ctx, "bucket", "key", reader, "application/octet-stream")
versionID, err := client.PutObjectBytesVersion(ctx, "bucket", "key", data, "text/plain")

// Read
body, err := client.GetObject(ctx, "bucket", "key")   // caller must close
data, err := client.GetObjectBytes(ctx, "bucket", "key")
body, err := client.GetObjectVersion(ctx, "bucket", "key", "version-id")
data, err := client.GetObjectVersionBytes(ctx, "bucket", "key", "version-id")
body, err := client.GetObjectVersionByNumber(ctx, "bucket", "key", 2)
data, err := client.GetObjectVersionBytesByNumber(ctx, "bucket", "key", 2)

// Delete
err := client.DeleteObject(ctx, "bucket", "key")
err := client.DeleteObjectVersion(ctx, "bucket", "key", "version-id")
err := client.DeleteObjectVersionByNumber(ctx, "bucket", "key", 2)
err := client.DeleteObjects(ctx, "bucket", []string{"key1", "key2"})  // auto-batches >1000 keys
err := client.DeleteObjectVersions(ctx, "bucket", []s3kit.ObjectVersionIdentifier{
    {Key: "key1", VersionID: "version-id-1"},
    {Key: "key2", VersionID: "version-id-2"},
})
err := client.DeleteObjectVersionsByNumber(ctx, "bucket", []s3kit.ObjectVersionNumberIdentifier{
    {Key: "key1", VersionNumber: 1},
    {Key: "key2", VersionNumber: 2},
})
err := client.DeleteAllObjectVersions(ctx, "bucket", "key")

// Query
exists, err := client.ObjectExists(ctx, "bucket", "key")
exists, err := client.ObjectVersionExists(ctx, "bucket", "key", "version-id")
exists, err := client.ObjectVersionExistsByNumber(ctx, "bucket", "key", 2)
keys,   err := client.ListObjects(ctx, "bucket", "prefix/")  // auto-paginates, returns all objects
versions, err := client.ListObjectVersions(ctx, "bucket", "key")
versionID, err := client.ObjectVersionID(ctx, "bucket", "key", 2)

// Transform
err := client.CopyObject(ctx, "src-bucket", "src-key", "dst-bucket", "dst-key")
versionID, err := client.CopyObjectVersion(ctx, "src-bucket", "src-key", "version-id", "dst-bucket", "dst-key")
versionID, err := client.CopyObjectVersionByNumber(ctx, "src-bucket", "src-key", 2, "dst-bucket", "dst-key")
err := client.MoveObject(ctx, "bucket", "old-key", "new-key")
versionID, err := client.MoveObjectVersion(ctx, "bucket", "old-key", "version-id", "new-key")
versionID, err := client.MoveObjectVersionByNumber(ctx, "bucket", "old-key", 2, "new-key")
```

### File Transfer

```go
// Content-type is detected automatically from the file extension
err := client.UploadFile(ctx, "bucket", "key", "/path/to/file.jpg")
versionID, err := client.UploadFileVersion(ctx, "bucket", "key", "/path/to/file.jpg")

// Atomic: creates parent directories, writes to a temp file first, renames on success
err := client.DownloadFile(ctx, "bucket", "key", "/path/to/dest.jpg")
err := client.DownloadFileVersion(ctx, "bucket", "key", "/path/to/dest.jpg", "version-id")
err := client.DownloadFileVersionByNumber(ctx, "bucket", "key", "/path/to/dest.jpg", 2)
```

### Presigned URLs

```go
url, err := client.PresignGetObject(ctx, "bucket", "key", time.Hour)
url, err := client.PresignGetObjectVersion(ctx, "bucket", "key", "version-id", time.Hour)
url, err := client.PresignGetObjectVersionByNumber(ctx, "bucket", "key", 2, time.Hour)
url, err := client.PresignPutObject(ctx, "bucket", "key", 15*time.Minute)
```

## Development

```bash
go test ./...
go vet ./...
go mod tidy
```

CI also runs `golangci-lint`, `govulncheck`, and a `go mod tidy` drift check on every pull request.

## Versioning Policy

- The module target is Go 1.26 and the codebase prefers current Go syntax and standard library APIs.
- The current public line is `v0.x`. In Go module semantics, `v0` means the API is still stabilizing and compatibility promises are intentionally weaker.
- New public releases are published by pushing a semantic version tag such as `v0.0.2`, `v0.1.0`, or `v1.0.0`. That tag is what Go tooling and `pkg.go.dev` index.
- For the current `v0` line:
  - `v0.0.z` should be used for fixes and low-risk adjustments.
  - `v0.y.0` may include API reshaping while the package is still being validated.
  - even in `v0`, avoid unnecessary breakage because early adopters still pin these versions in production.
- Once the exported API and behavior are intentionally stable, cut `v1.0.0`. From that point on, `v1` patch and minor releases should remain backward compatible.
- Breaking changes after `v1.0.0` must ship in a new major module path such as `github.com/kzzan/s3kit/v2`, not by rewriting `v1`.
- Dependency upgrades should default to `go get -u=patch ./...` plus a full test run.
- Pre-v1 modules require extra review because SemVer compatibility guarantees are weaker before v1.0.0.

## Release Process

```bash
git tag v0.0.2
git push origin v0.0.2
```

The release workflow validates the tag, creates a GitHub Release, and warms the Go module proxy so the new version is picked up by `pkg.go.dev` without changing older published versions.

## License

[Mozilla Public License 2.0](LICENSE)

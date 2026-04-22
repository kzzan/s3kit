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
- `ListObjects` auto-paginates — returns all objects regardless of count
- `DeleteObjects` auto-batches — handles any number of keys (1000/request limit handled internally)
- File upload/download with automatic content-type detection (100+ extensions)
- `DownloadFile` is atomic — no partial files left on failure
- Presigned URLs for GET and PUT
- Multipart upload/download via `aws-sdk-go-v2/feature/s3/transfermanager`
- Minimal API surface — one struct, zero global state

## Installation

```bash
go get github.com/kzzan/s3kit
```

Requires Go 1.26+.

## Quick Start

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
        Endpoint:        "https://s3.amazonaws.com",
        AccessKeyID:     "your-access-key",
        SecretAccessKey: "your-secret-key",
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

## Configuration

| Field | Description | Default |
|---|---|---|
| `Endpoint` | S3-compatible endpoint URL | required |
| `AccessKeyID` | Access key / username | required |
| `SecretAccessKey` | Secret key / password | required |
| `Region` | Region name | `us-east-1` |

**MinIO**

```go
client, err := s3kit.New(s3kit.Config{
    Endpoint:        "http://localhost:9000",
    AccessKeyID:     "minioadmin",
    SecretAccessKey: "minioadmin",
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
```

### Object Operations

```go
// Write
err := client.PutObject(ctx, "bucket", "key", reader, "application/octet-stream")
err := client.PutObjectBytes(ctx, "bucket", "key", data, "text/plain")

// Read
body, err := client.GetObject(ctx, "bucket", "key")   // caller must close
data, err := client.GetObjectBytes(ctx, "bucket", "key")

// Delete
err := client.DeleteObject(ctx, "bucket", "key")
err := client.DeleteObjects(ctx, "bucket", []string{"key1", "key2"})  // auto-batches >1000 keys

// Query
exists, err := client.ObjectExists(ctx, "bucket", "key")
keys,   err := client.ListObjects(ctx, "bucket", "prefix/")  // auto-paginates, returns all objects

// Transform
err := client.CopyObject(ctx, "src-bucket", "src-key", "dst-bucket", "dst-key")
err := client.MoveObject(ctx, "bucket", "old-key", "new-key")
```

### File Transfer

```go
// Content-type is detected automatically from the file extension
err := client.UploadFile(ctx, "bucket", "key", "/path/to/file.jpg")

// Atomic: writes to a temp file first, renames on success, cleans up on failure
err := client.DownloadFile(ctx, "bucket", "key", "/path/to/dest.jpg")
```

### Presigned URLs

```go
url, err := client.PresignGetObject(ctx, "bucket", "key", time.Hour)
url, err := client.PresignPutObject(ctx, "bucket", "key", 15*time.Minute)
```

## License

[Mozilla Public License 2.0](LICENSE)

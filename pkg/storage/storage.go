// Package storage is the bee result storage and safe download of the
// original by the url from a job. Implementations live in subpackages: local (a folder on
// disk) and s3 (any s3 provider); one bee always writes to one
// storage
package storage

import (
	"context"
	"io"
)

// Storage is what bee needs from a storage. Keys are relative to the storage
// root (the implementation adds the bucket prefix). Each object is written
// once under a new key, so the implementation sets a permanent
// cache on it (immutable)
type Storage interface {
	// Put writes one object; size is -1 if unknown
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error

	// Exists reports whether the object exists; absence is false without an error
	Exists(ctx context.Context, key string) (bool, error)

	// Ping checks that the storage is available (readiness)
	Ping(ctx context.Context) error
}

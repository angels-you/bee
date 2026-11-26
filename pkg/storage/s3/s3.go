package s3

import (
	"context"
	"io"
	"path"

	"github.com/angels-you/bee/pkg/tools"
)

// S3 is a bee storage in one bucket; implements storage.Storage. Prefix is
// the folder inside the bucket that holds all keys (empty means the root)
type S3 struct {
	bucket *Bucket
	prefix string
	cache  string // Cache-Control of every object: keys are always new, an object never changes
}

// New returns a storage in the creds bucket under prefix; the bucket is checked with Ping
func New(creds Creds, prefix string) (*S3, error) {
	bucket := NewBucket(creds)

	err := bucket.Ping()
	if err != nil {
		return nil, err
	}

	return NewWith(bucket, prefix), nil
}

// NewWith returns a storage on top of an already opened bucket
func NewWith(bucket *Bucket, prefix string) *S3 {
	a := &S3{
		bucket: bucket,
		prefix: tools.CleanKey(prefix),
	}
	a.init()

	return a
}

// init sets constant storage values
func (a *S3) init() {
	a.cache = "public, max-age=31536000, immutable"
}

// Put writes an object with a permanent cache
func (a *S3) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	err := ctx.Err()
	if err != nil {
		return err
	}

	meta := Meta{
		ContentType:  contentType,
		CacheControl: a.cache,
	}
	_, err = a.bucket.UploadReader(a.key(key), body, size, meta)

	return err
}

// Exists reports whether the object exists; absence is false without an error
func (a *S3) Exists(ctx context.Context, key string) (bool, error) {
	err := ctx.Err()
	if err != nil {
		return false, err
	}

	return a.bucket.Exists(a.key(key))
}

// Ping checks that the bucket is available
func (a *S3) Ping(ctx context.Context) error {
	err := ctx.Err()
	if err != nil {
		return err
	}

	return a.bucket.Ping()
}

// key returns the full object key: prefix/key
func (a *S3) key(key string) string {
	key = tools.CleanKey(key)

	if a.prefix == "" {
		return key
	}

	return path.Join(a.prefix, key)
}

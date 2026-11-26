package s3

import (
	"time"

	"github.com/minio/minio-go/v7"
)

// File is a bucket object without a body
type File struct {
	Path        string
	Size        int64
	ContentType string
	ETag        string
	Modified    time.Time
}

// Meta holds object headers on upload: ContentType, CacheControl etc.
type Meta = minio.PutObjectOptions

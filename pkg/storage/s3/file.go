package s3

import (
	"time"

	"github.com/minio/minio-go/v7"
)

// File — объект бакета без тела
type File struct {
	Path        string
	Size        int64
	ContentType string
	ETag        string
	Modified    time.Time
}

// Meta — заголовки объекта при загрузке: ContentType, CacheControl и т.д.
type Meta = minio.PutObjectOptions

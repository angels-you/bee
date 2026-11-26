package s3

import (
	"context"
	"io"
	"path"

	"github.com/angels-you/bee/pkg/tools"
)

// S3 — хранилище bee в одном бакете; реализует storage.Storage. Prefix —
// папка внутри бакета, под которой лежат все ключи (пусто — корень)
type S3 struct {
	bucket *Bucket
	prefix string
	cache  string // Cache-Control каждого объекта: ключи всегда новые, объект не меняется
}

// New — хранилище в бакете creds под prefix; бакет проверяется Ping
func New(creds Creds, prefix string) (*S3, error) {
	bucket := NewBucket(creds)

	err := bucket.Ping()
	if err != nil {
		return nil, err
	}

	return NewWith(bucket, prefix), nil
}

// NewWith — хранилище поверх уже открытого бакета
func NewWith(bucket *Bucket, prefix string) *S3 {
	a := &S3{
		bucket: bucket,
		prefix: tools.CleanKey(prefix),
	}
	a.init()

	return a
}

// init — постоянные значения хранилища
func (a *S3) init() {
	a.cache = "public, max-age=31536000, immutable"
}

// Put — запись объекта с вечным кэшем
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

// Exists — объект есть; отсутствие — false без ошибки
func (a *S3) Exists(ctx context.Context, key string) (bool, error) {
	err := ctx.Err()
	if err != nil {
		return false, err
	}

	return a.bucket.Exists(a.key(key))
}

// Ping — бакет доступен
func (a *S3) Ping(ctx context.Context) error {
	err := ctx.Err()
	if err != nil {
		return err
	}

	return a.bucket.Ping()
}

// key — полный ключ объекта: prefix/key
func (a *S3) key(key string) string {
	key = tools.CleanKey(key)

	if a.prefix == "" {
		return key
	}

	return path.Join(a.prefix, key)
}

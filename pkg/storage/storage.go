// Package storage — хранилище результатов bee и безопасная загрузка
// оригинала по url из job. Реализации лежат в подпакетах: local (папка на
// диске) и s3 (любой s3 провайдер); один bee всегда пишет в одно
// хранилище
package storage

import (
	"context"
	"io"
)

// Storage — то, что bee нужно от хранилища. Ключи относительные, от корня
// хранилища (префикс бакета добавляет реализация). Каждый объект пишется
// один раз под новым ключом, поэтому реализация сама ставит ему вечный
// кэш (immutable)
type Storage interface {
	// Put — записать один объект; size -1, если размер неизвестен
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error

	// Exists — объект есть; отсутствие — false без ошибки
	Exists(ctx context.Context, key string) (bool, error)

	// Ping — хранилище доступно (readiness)
	Ping(ctx context.Context) error
}

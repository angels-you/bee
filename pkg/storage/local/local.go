// Package local — хранилище bee в папке на диске: для локального запуска
// и тестов без s3
package local

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/angels-you/bee/pkg/tools"
)

// Local — файлы под Dir; реализует storage.Storage
type Local struct {
	Dir string
}

// New — хранилище в папке dir, папка создаётся при необходимости
func New(dir string) (*Local, error) {
	if dir == "" {
		return nil, errors.New("storage: local dir is required")
	}

	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return nil, err
	}

	return &Local{Dir: dir}, nil
}

// Put — запись через временный файл и rename: объект появляется целиком
func (a *Local) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	err := ctx.Err()
	if err != nil {
		return err
	}

	// папка объекта
	name := a.file(key)
	err = os.MkdirAll(filepath.Dir(name), 0o755)
	if err != nil {
		return err
	}

	// временный файл
	tmp := name + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	// тело
	_, err = io.Copy(f, body)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}

	err = f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}

	// готовый объект
	return os.Rename(tmp, name)
}

// Exists — файл есть; отсутствие — false без ошибки
func (a *Local) Exists(ctx context.Context, key string) (bool, error) {
	err := ctx.Err()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(a.file(key))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	}

	return false, err
}

// Ping — папка есть и это папка
func (a *Local) Ping(ctx context.Context) error {
	err := ctx.Err()
	if err != nil {
		return err
	}

	info, err := os.Stat(a.Dir)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return errors.New("storage: local dir is not a directory")
	}

	return nil
}

// file — путь файла по ключу, ключ всегда внутри Dir
func (a *Local) file(key string) string {
	key = tools.CleanKey(key)

	return filepath.Join(a.Dir, filepath.FromSlash(key))
}

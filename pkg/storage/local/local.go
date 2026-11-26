// Package local is a bee storage in a folder on disk: for local runs
// and tests without s3
package local

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/angels-you/bee/pkg/tools"
)

// Local stores files under Dir; implements storage.Storage
type Local struct {
	Dir string
}

// New returns a storage in folder dir, the folder is created if needed
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

// Put writes through a temporary file and rename: the object appears whole
func (a *Local) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	err := ctx.Err()
	if err != nil {
		return err
	}

	// object folder
	name := a.file(key)
	err = os.MkdirAll(filepath.Dir(name), 0o755)
	if err != nil {
		return err
	}

	// temporary file
	tmp := name + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	// body
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

	// finished object
	return os.Rename(tmp, name)
}

// Exists reports whether the file exists; absence is false without an error
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

// Ping checks that the folder exists and is a folder
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

// file returns the file path for a key, the key always stays inside Dir
func (a *Local) file(key string) string {
	key = tools.CleanKey(key)

	return filepath.Join(a.Dir, filepath.FromSlash(key))
}

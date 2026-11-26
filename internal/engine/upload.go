package engine

import (
	"bytes"
	"context"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/angels-you/bee/internal/encrypt"
	"github.com/angels-you/bee/pkg/types/code"
)

// upload is one object to publish: a local file or bytes in memory
type upload struct {
	key     string
	file    string       // local file; empty if data is set
	data    []byte       // made in memory (playlist, json)
	encrypt *encrypt.HLS // HLS segment of a job with a key: encrypted in memory right before Put
	track   *track       // HLS variant this init or segment belongs to; nil for the rest
	mime    string       // Content-Type; empty means by key extension
}

// track holds the published files of one HLS variant: sizes after
// encryption and the IV of each encrypted segment, by file name
type track struct {
	sizes map[string]int64
	ivs   map[string][]byte
}

// newTrack returns an empty variant
func newTrack() *track {
	return &track{
		sizes: map[string]int64{},
		ivs:   map[string][]byte{},
	}
}

// putAll runs the uploads of one wave, BEE_UPLOAD_CONCURRENCY at a time;
// the first error cancels the rest
func (a *work) putAll(ctx context.Context, list []upload) *code.Error {
	t := time.Now()
	defer a.took(&a.uploadMs, t)

	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// wave
	a.failed = nil
	sem := make(chan struct{}, a.engine.cfg.UploadConcurrency)
	var wg sync.WaitGroup
	for _, u := range list {
		wg.Add(1)
		go a.putOne(wctx, cancel, u, sem, &wg)
	}
	wg.Wait()

	// wave outcome
	err := a.failed
	if err == nil {
		err = ctx.Err()
	}

	if err == nil {
		return nil
	}

	return a.storageError(ctx, "upload", err)
}

// putOne runs one upload when a slot is free; the error is recorded once
// and cancels the wave
func (a *work) putOne(ctx context.Context, cancel context.CancelFunc, u upload, sem chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return
	}

	err := a.put(ctx, u)
	<-sem
	if err == nil {
		return
	}

	a.mu.Lock()
	if a.failed == nil && ctx.Err() == nil {
		a.failed = err
	}
	a.mu.Unlock()

	cancel()
}

// put writes one object with its Content-Type: a job segment with a key is encrypted
// in memory with a new IV, bytes from memory as is, a file streamed from disk
func (a *work) put(ctx context.Context, u upload) error {
	switch {
	case u.encrypt != nil:
		return a.putEncrypted(ctx, u)
	case u.data != nil:
		return a.putBytes(ctx, u, u.data, nil)
	}

	return a.putFile(ctx, u)
}

// putEncrypted writes a whole segment (segments are small), encrypted with
// AES-128-CBC using the job key and a new random IV
func (a *work) putEncrypted(ctx context.Context, u upload) error {
	plain, err := os.ReadFile(u.file)
	if err != nil {
		return err
	}

	data, iv, err := u.encrypt.Segment(plain)
	if err != nil {
		return err
	}

	return a.putBytes(ctx, u, data, iv)
}

// putBytes writes the data and records its size (and the encryption IV)
func (a *work) putBytes(ctx context.Context, u upload, data, iv []byte) error {
	err := a.engine.storage.Put(ctx, u.key, bytes.NewReader(data), int64(len(data)), u.contentType())
	if err != nil {
		return err
	}

	a.record(u, int64(len(data)), iv)
	return nil
}

// putFile streams a local file to storage and records its size
func (a *work) putFile(ctx context.Context, u upload) error {
	f, err := os.Open(u.file)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	err = a.engine.storage.Put(ctx, u.key, f, info.Size(), u.contentType())
	if err != nil {
		return err
	}

	a.record(u, info.Size(), nil)
	return nil
}

// record accounts a published object: bytes are counted; for an HLS file its size
// and the IV of the encrypted segment are recorded in the variant under the file name
func (a *work) record(u upload, size int64, iv []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.uploaded += size
	if u.track == nil {
		return
	}

	name := path.Base(u.key)
	u.track.sizes[name] = size
	if iv != nil {
		u.track.ivs[name] = iv
	}
}

// contentType returns the object Content-Type by key extension
func (a upload) contentType() string {
	if a.mime != "" {
		return a.mime
	}

	switch strings.ToLower(path.Ext(a.key)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".mp4":
		return "video/mp4"
	case ".m4s":
		return "video/iso.segment"
	case ".ts":
		return "video/mp2t"
	case ".json":
		return "application/json"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	}

	return "application/octet-stream"
}

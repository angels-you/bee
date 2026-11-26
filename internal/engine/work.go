package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/angels-you/bee/pkg/proto"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/angels-you/bee/pkg/storage"
	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/code"
)

// work is the shared state of one job of any type: slot, timeout, folder,
// uploads, metrics, logs
type work struct {
	engine *Engine
	kind   string         // audio, stream, video, images
	id     any            // job id as sent: a string or a number
	meta   map[string]any // data of the caller, returned untouched, never logged
	input  string         // source url, logged without query
	output string         // job entry object (master playlist, mp4); empty for images
	start  time.Time
	dir    string // job temp folder
	log    *slog.Logger

	// downloads
	downloaded int64
	downloads  context.Context // context of all job downloads: BEE_DOWNLOAD_TIMEOUT

	// uploads
	mu       sync.Mutex
	uploaded int64 // bytes uploaded
	failed   error // first error of the wave

	// stage timings for logs
	downloadMs, processMs, uploadMs int64
}

// newWork creates the job state; the job is counted as accepted
func (a *Engine) newWork(kind proto.Kind, id any, input string, meta map[string]any) *work {
	a.Metrics.started(string(kind))

	return &work{
		engine: a,
		kind:   string(kind),
		id:     id,
		meta:   meta,
		input:  input,
		start:  time.Now(),
		log:    a.log.With("job_id", tools.IDString(id), "worker_id", a.cfg.WorkerID, "type", kind),
	}
}

// begin takes a slot, the type timeout and the temp folder; end releases all of them.
// The returned context is the job context
func (a *work) begin(ctx context.Context, timeout time.Duration) (jctx context.Context, end func(), e *code.Error) {
	e = a.engine.acquire(ctx)
	if e != nil {
		return nil, nil, e
	}

	// job and download timeouts
	jctx, cancel := context.WithTimeout(ctx, timeout)
	downloads, cancelDownloads := context.WithTimeout(jctx, a.engine.cfg.DownloadTimeout)
	a.downloads = downloads

	// temp folder
	dir, err := os.MkdirTemp(a.engine.cfg.TempDir, a.kind+"-")
	if err != nil {
		cancelDownloads()
		cancel()
		a.engine.release()
		return nil, nil, code.New(code.InternalTemporaryError, "temp dir", err)
	}
	a.dir = dir

	end = func() {
		os.RemoveAll(dir)
		cancelDownloads()
		cancel()
		a.engine.release()
	}
	return jctx, end, nil
}

// fetch downloads one url (field names it in errors) to <dir>/<name>
// within BEE_DOWNLOAD_TIMEOUT for all job downloads
func (a *work) fetch(ctx context.Context, field, rawURL, name string) (string, storage.Info, *code.Error) {
	t := time.Now()
	defer a.took(&a.downloadMs, t)

	// download
	file := filepath.Join(a.dir, name)
	info, err := a.engine.down[proto.Kind(a.kind)].Download(a.downloads, rawURL, file)
	if err == nil {
		a.downloaded += info.Size
		a.engine.Metrics.transferred(info.Size, 0)
		return file, info, nil
	}

	// the job context ended before the downloads
	if ctx.Err() != nil {
		return "", info, code.As(ctx.Err())
	}

	// the downloader error already has a code, add the field
	e := code.As(err)
	return "", info, code.New(e.Code, field+": "+e.Message, e.Err)
}

// took adds the time since t to the stage counter
func (a *work) took(ms *int64, t time.Time) {
	*ms += time.Since(t).Milliseconds()
}

// storageError maps a failed storage call: timeout is STORAGE_TIMEOUT,
// an ended job context is returned as is, anything else is STORAGE_UNAVAILABLE
func (a *work) storageError(ctx context.Context, what string, err error) *code.Error {
	var ne net.Error

	switch {
	case ctx.Err() != nil:
		return code.As(ctx.Err())
	case errors.Is(err, context.DeadlineExceeded):
		return code.New(code.StorageTimeout, what+" timed out", err)
	case errors.As(err, &ne) && ne.Timeout():
		return code.New(code.StorageTimeout, what+" timed out", err)
	}

	return code.New(code.StorageUnavailable, what+" failed", err)
}

// stored reports that every key is in storage
func (a *work) stored(ctx context.Context, keys []string) *code.Error {
	for _, key := range keys {
		ok, err := a.engine.storage.Exists(ctx, key)
		if err != nil {
			return a.storageError(ctx, "storage check", err)
		}

		if !ok {
			return code.New(code.StorageUnavailable, key+" is missing after its upload", nil)
		}
	}

	return nil
}

// done handles a successful job: metrics and log
func (a *work) done(attrs ...any) {
	elapsed := time.Since(a.start)
	a.engine.Metrics.finished(a.kind, "", elapsed)
	a.engine.Metrics.transferred(0, a.uploaded)

	attrs = append(attrs,
		"download_bytes", a.downloaded,
		"upload_bytes", a.uploaded,
		"download_ms", a.downloadMs,
		"process_ms", a.processMs,
		"upload_ms", a.uploadMs,
		"elapsed_ms", elapsed.Milliseconds(),
	)
	a.log.Info("engine: done", attrs...)
}

// fail handles a failed job: metrics and log without the url query
func (a *work) fail(e *code.Error) {
	elapsed := time.Since(a.start)
	a.engine.Metrics.finished(a.kind, e.Code, elapsed)
	a.engine.Metrics.transferred(0, a.uploaded)

	a.log.Warn("engine: failed",
		"error_code", string(e.Code),
		"retryable", e.Retryable,
		"error", e.Error(),
		"source", tools.SafeURL(a.input),
		"download_ms", a.downloadMs,
		"process_ms", a.processMs,
		"upload_ms", a.uploadMs,
		"elapsed_ms", elapsed.Milliseconds(),
	)
}

// elapsed returns milliseconds since the job started
func (a *work) elapsed() int64 {
	return time.Since(a.start).Milliseconds()
}

// processing adds processing time (ffmpeg, vips) since t to the job counter and metrics
func (a *work) processing(t time.Time) {
	elapsed := time.Since(t)
	a.processMs += elapsed.Milliseconds()
	a.engine.Metrics.processed(a.kind, elapsed)
}

// checkMeta refuses a meta bigger than the limit of the engine in json
func (a *work) checkMeta() *code.Error {
	if a.meta == nil {
		return nil
	}

	data, err := json.Marshal(a.meta)
	switch {
	case err != nil:
		return code.New(code.InvalidJob, "meta is not json", nil)
	case len(data) > a.engine.metaLimit:
		return code.New(code.InvalidJob, fmt.Sprintf("meta is %d bytes of json, more than %d", len(data), a.engine.metaLimit), nil)
	}

	return nil
}

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// work — общее состояние одной job любого типа: слот, таймаут, папка,
// загрузки, журнал, метрики, логи
type work struct {
	engine *Engine
	kind   string // audio, stream, video, images
	id     string
	input  string // url оригинала, в логах без query
	output string // входной объект job (ключ журнала); пусто у картинок
	start  time.Time
	dir    string // временная папка job
	log    *slog.Logger

	// скачивания
	downloaded int64
	downloads  context.Context // контекст всех скачиваний job: BEE_DOWNLOAD_TIMEOUT

	// загрузки
	mu       sync.Mutex
	uploaded int64 // байт загружено
	failed   error // первая ошибка волны

	// время этапов для логов
	downloadMs, processMs, uploadMs int64
}

// newWork — состояние job, она посчитана как принятая
func (a *Engine) newWork(kind proto.Kind, id, input string) *work {
	a.Metrics.started(string(kind))

	return &work{
		engine: a,
		kind:   string(kind),
		id:     id,
		input:  input,
		start:  time.Now(),
		log:    a.log.With("job_id", id, "worker_id", a.cfg.WorkerID, "type", kind),
	}
}

// begin — слот, таймаут типа и временная папка; end освобождает всё.
// Вернувшийся контекст — контекст job
func (a *work) begin(ctx context.Context, timeout time.Duration) (jctx context.Context, end func(), e *code.Error) {
	e = a.engine.acquire(ctx)
	if e != nil {
		return nil, nil, e
	}

	// таймаут job и скачиваний
	jctx, cancel := context.WithTimeout(ctx, timeout)
	downloads, cancelDownloads := context.WithTimeout(jctx, a.engine.cfg.DownloadTimeout)
	a.downloads = downloads

	// временная папка
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

// fetch — один url (field называет его в ошибках) в <dir>/<name> в
// пределах BEE_DOWNLOAD_TIMEOUT всех скачиваний job
func (a *work) fetch(ctx context.Context, field, rawURL, name string) (string, storage.Info, *code.Error) {
	t := time.Now()
	defer a.took(&a.downloadMs, t)

	// скачиваем
	file := filepath.Join(a.dir, name)
	info, err := a.engine.down[proto.Kind(a.kind)].Download(a.downloads, rawURL, file)
	if err == nil {
		a.downloaded += info.Size
		a.engine.Metrics.transferred(info.Size, 0)
		return file, info, nil
	}

	// контекст job закончился раньше скачиваний
	if ctx.Err() != nil {
		return "", info, code.As(ctx.Err())
	}

	// ошибка загрузчика уже с кодом, добавляем поле
	e := code.As(err)
	return "", info, code.New(e.Code, field+": "+e.Message, e.Err)
}

// took — время с t прибавляется к счётчику этапа
func (a *work) took(ms *int64, t time.Time) {
	*ms += time.Since(t).Milliseconds()
}

// storageError — неудачный вызов хранилища: таймаут — STORAGE_TIMEOUT,
// закончившийся контекст job — как есть, остальное — STORAGE_UNAVAILABLE
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

// published — входной объект уже в хранилище? Нет — job работает. Да —
// результат этой же job из журнала (done true), иначе ALREADY_EXISTS:
// новый запуск — новые ключи. res — результат в json
func (a *work) published(ctx context.Context) (res []byte, done bool, e *code.Error) {
	exists, err := a.engine.storage.Exists(ctx, a.output)
	if err != nil {
		return nil, false, a.storageError(ctx, "output check", err)
	}

	if !exists {
		return nil, false, nil
	}

	// чей это объект
	taken := code.New(code.AlreadyExists, "an object is already stored at output", nil)
	ledger := a.engine.ledger
	if ledger == nil {
		return nil, false, taken
	}

	res, found, err := ledger.Load(ctx, a.ledgerKey())
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, false, code.As(ctx.Err())
	case err != nil:
		return nil, false, code.New(code.InternalTemporaryError, "ledger lookup failed", err)
	case !found:
		return nil, false, taken
	}

	return res, true, nil
}

// remember — результат в журнал (если он есть) перед публикацией входного
// объекта; ошибка оставляет вход неопубликованным
func (a *work) remember(ctx context.Context, res any) *code.Error {
	ledger := a.engine.ledger
	if ledger == nil {
		return nil
	}

	data, err := json.Marshal(res)
	if err != nil {
		return code.New(code.InternalTemporaryError, "result json", err)
	}

	err = ledger.Save(ctx, a.ledgerKey(), data)
	if err == nil {
		return nil
	}

	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	return code.New(code.InternalTemporaryError, "ledger save failed", err)
}

// ledgerKey — ключ журнала: sha256 типа, id и входного объекта
func (a *work) ledgerKey() string {
	sum := sha256.Sum256([]byte(a.kind + "\n" + a.id + "\n" + a.output))

	return hex.EncodeToString(sum[:])
}

// stored — каждый ключ есть в хранилище
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

// done — успешная job: метрики и лог
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

// again — сохранённый результат job, уже выполненной раньше
func (a *work) again() {
	elapsed := time.Since(a.start)
	a.engine.Metrics.finished(a.kind, "", elapsed)

	a.log.Info("engine: already done, the stored result is returned", "elapsed_ms", elapsed.Milliseconds())
}

// fail — упавшая job: метрики и лог без query url
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

// elapsed — миллисекунд с начала job
func (a *work) elapsed() int64 {
	return time.Since(a.start).Milliseconds()
}

// processing — обработка (ffmpeg, vips) с t: в счётчик job и в метрики
func (a *work) processing(t time.Time) {
	elapsed := time.Since(t)
	a.processMs += elapsed.Milliseconds()
	a.engine.Metrics.processed(a.kind, elapsed)
}

// Package engine — ядро bee: слоты concurrency и очередь QueueWait,
// таймаут job, временная папка, загрузка оригинала, загрузка результатов
// волнами, журнал результатов и метрики, общие для всех типов. Сами
// пайплайны — audio.go (ffmpeg, HLS аудио), video.go (ffmpeg, HLS и MP4),
// images.go (vips); инструменты обработки — в подпакетах audio, video,
// images. Транспорты вызывают только Audio, Stream, Video и Images.
//
// Параметры обработки job накладываются поверх дефолтов конфига, лимиты
// сервера берутся только из конфига
package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/angels-you/bee/pkg/proto"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/angels-you/bee/internal/config"
	audiotool "github.com/angels-you/bee/internal/engine/audio"
	imagetool "github.com/angels-you/bee/internal/engine/images"
	videotool "github.com/angels-you/bee/internal/engine/video"
	"github.com/angels-you/bee/pkg/storage"
	"github.com/angels-you/bee/pkg/types/code"
)

// Ledger — результаты готовых job, их хранит транспорт (nats — в
// key-value JetStream). Job сохраняет результат прямо перед публикацией
// своего входного объекта (master playlist, mp4). Job, которая находит
// вход уже опубликованным, ищет свой результат: нашла — job уже была
// выполнена (передоставка после потерянного ack или падения), результат
// возвращается снова; не нашла — ключи чужие: ALREADY_EXISTS
type Ledger interface {
	Save(ctx context.Context, key string, value []byte) error
	Load(ctx context.Context, key string) (value []byte, found bool, err error)
}

// Engine — выполняет job всех типов. Безопасен для одновременного
// использования; BEE_CONCURRENCY ограничивает job одновременно
type Engine struct {
	cfg     *config.Config
	storage storage.Storage
	ledger  Ledger
	log     *slog.Logger
	slots   chan struct{}
	down    map[proto.Kind]*storage.Downloader // по типу job: у каждого свой лимит размера

	audio  audiotool.Tools
	video  *videotool.Tools
	images imagetool.Tools

	Metrics *Metrics
}

// New — движок по конфигу: временная папка, проверка ffmpeg и vips.
// ledger может быть nil: тогда опубликованный вход всегда
// ALREADY_EXISTS
func New(cfg *config.Config, st storage.Storage, ledger Ledger, log *slog.Logger) (*Engine, error) {
	switch {
	case cfg == nil:
		return nil, errors.New("engine: config is required")
	case st == nil:
		return nil, errors.New("engine: storage is required")
	case log == nil:
		return nil, errors.New("engine: logger is required")
	}

	a := &Engine{
		cfg:     cfg,
		storage: st,
		ledger:  ledger,
		log:     log,
		slots:   make(chan struct{}, cfg.Concurrency),
		Metrics: &Metrics{},
	}
	a.Metrics.init()

	// временная папка
	err := os.MkdirAll(cfg.TempDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("engine: temp dir: %w", err)
	}

	// инструменты
	err = a.tools()
	if err != nil {
		return nil, err
	}

	// загрузчики оригиналов
	a.downloaders()
	return a, nil
}

// tools — ffmpeg для аудио и видео, vips для картинок; всё проверяется
// один раз при старте: чего нет — bee не стартует, а не роняет job
func (a *Engine) tools() error {
	cfg := a.cfg
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// fpcalc необязателен: без него fingerprint — предупреждение в job
	fpcalc := cfg.Tools.Fpcalc
	if fpcalc != "" {
		_, err := exec.LookPath(fpcalc)
		if err != nil {
			fpcalc = ""
		}
	}

	if cfg.Audio.Fingerprint && fpcalc == "" {
		return fmt.Errorf("engine: BEE_AUDIO_FINGERPRINT needs %s", cfg.Tools.Fpcalc)
	}

	if fpcalc == "" {
		a.log.Warn("engine: no fpcalc, audio fingerprint is off")
	}

	// аудио
	a.audio = audiotool.Tools{FFmpeg: cfg.Tools.FFmpeg, FFprobe: cfg.Tools.FFprobe, Fpcalc: fpcalc}
	err := a.audio.Check()
	if err != nil {
		return fmt.Errorf("engine: audio: %w", err)
	}

	// видео
	a.video = &videotool.Tools{FFmpeg: cfg.Tools.FFmpeg, FFprobe: cfg.Tools.FFprobe, Threads: cfg.Video.Threads}
	err = a.video.Check(ctx)
	if err != nil {
		return fmt.Errorf("engine: video: %w", err)
	}

	// картинки
	a.images = imagetool.Tools{
		Vips:       cfg.Tools.Vips,
		Header:     cfg.Tools.VipsHeader,
		Threads:    cfg.Images.Threads,
		AvifEffort: cfg.Images.AvifEffort,
	}
	err = a.images.Check(ctx, cfg.TempDir)
	if err != nil {
		return fmt.Errorf("engine: images: %w", err)
	}

	return nil
}

// downloaders — загрузчик оригиналов каждого типа со своим лимитом размера
func (a *Engine) downloaders() {
	cfg := a.cfg

	a.down = map[proto.Kind]*storage.Downloader{
		proto.Audio:  a.downloader(cfg.Audio.MaxSourceMB),
		proto.Stream: a.downloader(cfg.Video.MaxSourceMB),
		proto.Video:  a.downloader(cfg.Video.MaxSourceMB),
		proto.Images: a.downloader(cfg.Images.MaxSourceMB),
	}
}

// downloader — загрузчик с лимитом mb MiB
func (a *Engine) downloader(mb int64) *storage.Downloader {
	return &storage.Downloader{
		UserAgent:     "bee/1",
		MaxSize:       mb << 20,
		AllowInsecure: a.cfg.AllowInsecure,
		HostAllowed:   a.cfg.SourceHostAllowed,
	}
}

// Ready — ffmpeg и vips отвечают, хранилище доступно (readiness)
func (a *Engine) Ready(ctx context.Context) error {
	err := a.video.Version(ctx)
	if err != nil {
		return err
	}

	err = a.images.Version(ctx)
	if err != nil {
		return err
	}

	return a.storage.Ping(ctx)
}

// acquire — слот job; ждёт до BEE_QUEUE_WAIT
func (a *Engine) acquire(ctx context.Context) *code.Error {
	select {
	case a.slots <- struct{}{}:
		return nil
	default:
	}

	// все слоты заняты, ждать нельзя
	busy := code.New(code.InternalTemporaryError, "all job slots are busy", nil)
	if a.cfg.QueueWait == 0 {
		return busy
	}

	// ждём слот
	t := time.NewTimer(a.cfg.QueueWait)
	defer t.Stop()

	select {
	case a.slots <- struct{}{}:
		return nil
	case <-t.C:
		return busy
	case <-ctx.Done():
		return code.As(ctx.Err())
	}
}

// release — освобождает слот законченной job
func (a *Engine) release() {
	<-a.slots
}

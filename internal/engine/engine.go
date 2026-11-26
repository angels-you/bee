// Package engine is the core of bee: concurrency slots and the QueueWait queue,
// job timeout, temp folder, source download, result uploads in waves,
// the result ledger and metrics, shared by all types. The pipelines
// themselves are audio.go (ffmpeg, audio HLS), video.go (ffmpeg, HLS and MP4),
// images.go (vips); processing tools live in the subpackages audio, video,
// images. Transports call only Audio, Stream, Video and Images.
//
// Job processing parameters are applied over the config defaults, server
// limits come from the config only
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

// Ledger holds the results of finished jobs, kept by the transport (nats: in
// JetStream key-value). A job saves its result right before publishing
// its entry object (master playlist, mp4). A job that finds its entry
// already published looks up its result: found means the job was already
// done (redelivery after a lost ack or a crash) and the result is
// returned again; not found means the keys belong to someone else: ALREADY_EXISTS
type Ledger interface {
	Save(ctx context.Context, key string, value []byte) error
	Load(ctx context.Context, key string) (value []byte, found bool, err error)
}

// Engine runs jobs of all types. It is safe for concurrent use;
// BEE_CONCURRENCY limits how many jobs run at once
type Engine struct {
	cfg     *config.Config
	storage storage.Storage
	ledger  Ledger
	log     *slog.Logger
	slots   chan struct{}
	down    map[proto.Kind]*storage.Downloader // by job type: each has its own size limit

	audio  audiotool.Tools
	video  *videotool.Tools
	images imagetool.Tools

	Metrics *Metrics
}

// New builds the engine from the config: temp folder, ffmpeg and vips checks.
// ledger may be nil: then a published entry is always
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

	// temp folder
	err := os.MkdirAll(cfg.TempDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("engine: temp dir: %w", err)
	}

	// tools
	err = a.tools()
	if err != nil {
		return nil, err
	}

	// source downloaders
	a.downloaders()
	return a, nil
}

// tools checks ffmpeg for audio and video, vips for images; everything is checked
// once at startup: if something is missing bee does not start, rather than failing jobs
func (a *Engine) tools() error {
	cfg := a.cfg
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// fpcalc is optional: without it fingerprint is a warning in the job
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

	// audio
	a.audio = audiotool.Tools{FFmpeg: cfg.Tools.FFmpeg, FFprobe: cfg.Tools.FFprobe, Fpcalc: fpcalc}
	err := a.audio.Check()
	if err != nil {
		return fmt.Errorf("engine: audio: %w", err)
	}

	// video
	a.video = &videotool.Tools{FFmpeg: cfg.Tools.FFmpeg, FFprobe: cfg.Tools.FFprobe, Threads: cfg.Video.Threads}
	err = a.video.Check(ctx)
	if err != nil {
		return fmt.Errorf("engine: video: %w", err)
	}

	// images
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

// downloaders builds a source downloader per type with its own size limit
func (a *Engine) downloaders() {
	cfg := a.cfg

	a.down = map[proto.Kind]*storage.Downloader{
		proto.Audio:  a.downloader(cfg.Audio.MaxSourceMB),
		proto.Stream: a.downloader(cfg.Video.MaxSourceMB),
		proto.Video:  a.downloader(cfg.Video.MaxSourceMB),
		proto.Images: a.downloader(cfg.Images.MaxSourceMB),
	}
}

// downloader builds a downloader limited to mb MiB
func (a *Engine) downloader(mb int64) *storage.Downloader {
	return &storage.Downloader{
		UserAgent:     "bee/1",
		MaxSize:       mb << 20,
		AllowInsecure: a.cfg.AllowInsecure,
		HostAllowed:   a.cfg.SourceHostAllowed,
	}
}

// Ready reports that ffmpeg and vips respond and storage is reachable (readiness)
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

// acquire takes a job slot; waits up to BEE_QUEUE_WAIT
func (a *Engine) acquire(ctx context.Context) *code.Error {
	select {
	case a.slots <- struct{}{}:
		return nil
	default:
	}

	// all slots are busy, waiting is not allowed
	busy := code.New(code.InternalTemporaryError, "all job slots are busy", nil)
	if a.cfg.QueueWait == 0 {
		return busy
	}

	// wait for a slot
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

// release frees the slot of a finished job
func (a *Engine) release() {
	<-a.slots
}

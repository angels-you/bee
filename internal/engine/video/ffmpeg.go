// Package video makes the outputs of bee with the ffmpeg and ffprobe
// command line tools: probe the source, plan the timeline and the frame of
// every output, build one filter graph and encode every output in one
// ffmpeg run (HLS renditions or a single MP4, the preview, the
// thumbnails), then probe and check what was written. Every command runs
// through exec without a shell, reads only local files (protocol
// whitelist "file") and never touches the network.
package video

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	job "github.com/angels-you/bee/pkg/types/video"
)

// Tools — the ffmpeg and ffprobe binaries and what the ffmpeg build can
// do. Check must run first: it fills the lists of encoders, filters and
// muxers the Can* methods answer from, and the demuxers each kind of
// input may be read with.
type Tools struct {
	FFmpeg  string // a path or a name in PATH
	FFprobe string // a path or a name in PATH
	Threads int    // threads of every encoder and of the filter graph of one run, at least 1

	encoders map[string]bool
	filters  map[string]bool
	muxers   map[string]bool

	// the demuxers of each input (-format_whitelist): media containers and
	// still pictures only, no playlist or concat demuxer, so a crafted
	// input cannot make ffmpeg read any other file
	sourceFormats  string
	soundFormats   string
	pictureFormats string
}

// Version — ffmpeg and ffprobe answer (readiness).
func (a *Tools) Version(ctx context.Context) error {
	_, _, err := a.run(ctx, "", a.FFmpeg, "-hide_banner", "-version")
	if err != nil {
		return err
	}

	_, _, err = a.run(ctx, "", a.FFprobe, "-hide_banner", "-version")
	return err
}

// Check — reads the encoders, filters and muxers of the ffmpeg build and
// requires what every job needs: H.264, AAC, JPEG and PNG encoders, every
// filter the graph uses and the mp4, hls, mpegts and image2 muxers. HEVC,
// AV1, WebP, AVIF and HDR→SDR are optional: a job that needs a missing
// one is refused (CanEncode, CanWrite, CanTonemap). Run once at start: a
// missing part stops the worker instead of failing jobs.
func (a *Tools) Check(ctx context.Context) error {
	var err error

	// the demuxers of the inputs
	a.sourceFormats = "mov,matroska,avi,mpegts"
	a.soundFormats = "mp3,aac,mov,matroska,ogg,wav,flac,aiff"
	a.pictureFormats = "png_pipe,jpeg_pipe,webp_pipe"

	// ffprobe answers
	_, _, err = a.run(ctx, "", a.FFprobe, "-hide_banner", "-version")
	if err != nil {
		return err
	}

	// what the build has
	a.encoders, err = a.list(ctx, "-encoders")
	if err != nil {
		return err
	}
	a.filters, err = a.list(ctx, "-filters")
	if err != nil {
		return err
	}
	a.muxers, err = a.list(ctx, "-muxers")
	if err != nil {
		return err
	}

	// what every job needs
	encoders := []string{"libx264", "aac", "mjpeg", "png"}
	filters := []string{
		"split", "scale", "crop", "pad", "setsar", "fps", "format", "transpose", "hflip", "vflip",
		"eq", "hue", "unsharp", "gblur", "overlay", "colorchannelmixer", "setpts", "trim", "tpad",
		"asplit", "aformat", "volume", "atempo", "amix", "apad", "atrim", "asetpts", "ebur128", "afade",
	}
	muxers := []string{"mp4", "hls", "mpegts", "image2"}

	var errs []error
	for _, name := range encoders {
		if !a.encoders[name] {
			errs = append(errs, fmt.Errorf("video: ffmpeg has no %s encoder", name))
		}
	}
	for _, name := range filters {
		if !a.filters[name] {
			errs = append(errs, fmt.Errorf("video: ffmpeg has no %s filter", name))
		}
	}
	for _, name := range muxers {
		if !a.muxers[name] {
			errs = append(errs, fmt.Errorf("video: ffmpeg has no %s muxer", name))
		}
	}
	return errors.Join(errs...)
}

// CheckProduction — after Check: the optional parts production needs are
// there too (HEVC and AV1 encoders, WebP and AVIF thumbnails, HDR→SDR).
// The image build runs it (bee check), so an image without them is
// never built; a dev machine may lack them.
func (a *Tools) CheckProduction() error {
	var errs []error
	if !a.CanEncode(job.VideoHEVC) {
		errs = append(errs, errors.New("video: ffmpeg has no libx265 encoder (hevc)"))
	}
	if !a.CanEncode(job.VideoAV1) {
		errs = append(errs, errors.New("video: ffmpeg has no libsvtav1 encoder (av1)"))
	}
	if !a.CanWrite(job.ImageWebP) {
		errs = append(errs, errors.New("video: ffmpeg has no libwebp encoder (webp thumbnails)"))
	}
	if !a.CanWrite(job.ImageAVIF) {
		errs = append(errs, errors.New("video: ffmpeg has no libsvtav1 encoder or avif muxer (avif thumbnails)"))
	}
	if !a.CanTonemap() {
		errs = append(errs, errors.New("video: ffmpeg has no zscale or tonemap filter (HDR to SDR)"))
	}
	return errors.Join(errs...)
}

// list — the names ffmpeg prints for -encoders, -filters or -muxers: the
// second field of every entry line ("flags name description"). The title
// ("Filters:") and the dashes are one field; the legend lines read
// "flags = meaning" (ffmpeg 6 ends the legend of -filters without
// dashes, so the legend is told by its "=").
func (a *Tools) list(ctx context.Context, what string) (map[string]bool, error) {
	out, _, err := a.run(ctx, "", a.FFmpeg, "-hide_banner", what)
	if err != nil {
		return nil, err
	}

	names := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[1] == "=" {
			continue
		}
		names[fields[1]] = true
	}
	if sc.Err() != nil {
		return nil, sc.Err()
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("video: ffmpeg %s printed no names", what)
	}
	return names, nil
}

// CanEncode — the build has the encoder of the codec.
func (a *Tools) CanEncode(c job.VideoCodec) bool {
	return a.encoders[a.encoder(c)]
}

// CanWrite — the build has the encoder (and for avif the muxer) of the
// image format.
func (a *Tools) CanWrite(f job.ImageFormat) bool {
	switch f {
	case job.ImageJPEG:
		return a.encoders["mjpeg"]
	case job.ImagePNG:
		return a.encoders["png"]
	case job.ImageWebP:
		return a.encoders["libwebp"]
	case job.ImageAVIF:
		return a.encoders["libsvtav1"] && a.muxers["avif"]
	}
	return false
}

// CanTonemap — the build converts HDR (PQ, HLG) to SDR: zscale and
// tonemap filters (libzimg).
func (a *Tools) CanTonemap() bool {
	return a.filters["zscale"] && a.filters["tonemap"]
}

// encoder — the ffmpeg encoder of a video codec.
func (a *Tools) encoder(c job.VideoCodec) string {
	switch c {
	case job.VideoH264:
		return "libx264"
	case job.VideoHEVC:
		return "libx265"
	case job.VideoAV1:
		return "libsvtav1"
	}
	return ""
}

// run — one ffmpeg or ffprobe process in dir (empty = the current one);
// returns stdout and stderr (where ffmpeg logs, e.g. the ebur128
// summary). A finished context is returned as its own error; any other
// failure carries the end of stderr (local paths only, the tools never
// see a url). SVT_LOG=1 keeps the svt-av1 library to its errors.
func (a *Tools) run(ctx context.Context, dir, bin string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SVT_LOG=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), stderr.Bytes(), nil
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	// the cause is printed last
	msg := strings.ReplaceAll(strings.TrimSpace(stderr.String()), "\n", "; ")
	if len(msg) > 600 {
		msg = msg[len(msg)-600:]
	}
	return nil, nil, fmt.Errorf("%s: %w: %s", filepath.Base(bin), err, msg)
}

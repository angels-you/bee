package video

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/angels-you/bee/pkg/types/code"
)

// Report — every output of a run, measured: the renditions in plan order,
// the preview, the thumbnails in job order.
type Report struct {
	Renditions []Output
	Preview    *Output
	Stills     []Image
}

// Output — one encoded video output (an HLS rendition or an MP4) as
// ffprobe reads it back. Media.VideoBitrate and Media.AudioBitrate are the
// measured averages: the bytes of the packets of each stream over the
// duration. Size is the bytes on disk: the MP4, or the media playlist,
// init and segments of a rendition (before encryption). Playlist is set
// for HLS.
type Output struct {
	Media    Media
	Size     int64
	Playlist *Playlist
}

// Image — one thumbnail as written.
type Image struct {
	Width  int
	Height int
	Size   int64
}

// Verify — every output of the plan under dir read back and checked: the
// frame of its profile, the timeline duration, audio exactly when the
// plan has it, for HLS a VOD playlist with every segment present and the
// same cut in every rendition. A failed check is VERIFY_FAILED.
func (a *Tools) Verify(ctx context.Context, p *Plan, dir string) (*Report, *code.Error) {
	report := &Report{}

	// the renditions
	for _, r := range p.Renditions {
		out, e := a.output(ctx, dir, r.File, r.Dir != "")
		if e != nil {
			return nil, e
		}

		e = a.check(out, r.Profile.Width, r.Profile.Height, p.Duration, p.HasSound, r.File)
		if e != nil {
			return nil, e
		}
		report.Renditions = append(report.Renditions, out)
	}

	// HLS: one cut for all
	if p.HLS != nil {
		e := a.aligned(p, report.Renditions)
		if e != nil {
			return nil, e
		}
	}

	// the preview
	if p.Clip != nil {
		out, e := a.output(ctx, dir, p.Clip.Path, false)
		if e != nil {
			return nil, e
		}

		// sound only when the plan has it and the preview asks for it
		sound := p.HasSound && p.Clip.Preview.Sound
		e = a.check(out, p.Clip.Geometry.Width, p.Clip.Geometry.Height, p.Clip.Duration, sound, p.Clip.Path)
		if e != nil {
			return nil, e
		}
		report.Preview = &out
	}

	// the thumbnails
	for _, s := range p.Stills {
		m, err := a.probe(ctx, filepath.Join(dir, s.Path), "")
		if err != nil {
			return nil, a.fail(ctx, s.Path+" is not readable", err)
		}

		// the planned size
		size := Size{Width: m.Width, Height: m.Height}
		if size != s.Size {
			return nil, code.New(code.VerifyFailed, fmt.Sprintf("%s is %dx%d, planned %dx%d", s.Path, m.Width, m.Height, s.Size.Width, s.Size.Height), nil)
		}
		report.Stills = append(report.Stills, Image{Width: m.Width, Height: m.Height, Size: m.Size})
	}
	return report, nil
}

// output — one video output (name: its path under dir) read back: its
// probe, the bitrates measured from its packets, its size on disk and, for
// HLS, its playlist with every file of it present and not empty.
func (a *Tools) output(ctx context.Context, dir, name string, hls bool) (Output, *code.Error) {
	file := filepath.Join(dir, name)

	m, err := a.probe(ctx, file, "")
	if err != nil {
		return Output{}, a.fail(ctx, name+" is not readable", err)
	}
	out := Output{Media: m}

	// the measured bitrates
	video, audio, err := a.bytes(ctx, file)
	if err != nil {
		return Output{}, a.fail(ctx, name+": packets", err)
	}
	if m.Duration > 0 {
		out.Media.VideoBitrate = int(math.Round(float64(video) * 8 / m.Duration / 1000))
		out.Media.AudioBitrate = int(math.Round(float64(audio) * 8 / m.Duration / 1000))
	}

	// the size: the file, and for HLS every file of the playlist
	info, err := os.Stat(file)
	if err != nil {
		return Output{}, a.fail(ctx, name, err)
	}
	out.Size = info.Size()

	// an MP4 is done
	if !hls {
		return out, nil
	}

	// the playlist
	text, err := os.ReadFile(file)
	if err != nil {
		return Output{}, a.fail(ctx, name, err)
	}

	pl := &Playlist{}
	err = pl.Parse(string(text))
	if err != nil {
		return Output{}, code.New(code.VerifyFailed, name+": "+err.Error(), nil)
	}

	// a finished VOD playlist with segments
	switch {
	case !pl.VOD:
		return Output{}, code.New(code.VerifyFailed, name+" is not a VOD playlist", nil)
	case !pl.EndList:
		return Output{}, code.New(code.VerifyFailed, name+" has no EXT-X-ENDLIST", nil)
	case len(pl.Segments) == 0:
		return Output{}, code.New(code.VerifyFailed, name+" has no segments", nil)
	}

	// every file next to the playlist, by a plain name
	names := []string{}
	if pl.Init != "" {
		names = append(names, pl.Init)
	}
	for _, s := range pl.Segments {
		names = append(names, s.Name)
	}

	// each present and not empty
	for _, n := range names {
		if n != filepath.Base(n) {
			return Output{}, code.New(code.VerifyFailed, name+": "+n+" is not a plain file name", nil)
		}

		info, err := os.Stat(filepath.Join(filepath.Dir(file), n))
		switch {
		case err != nil:
			return Output{}, code.New(code.VerifyFailed, name+": "+n+" is missing", err)
		case info.Size() == 0:
			return Output{}, code.New(code.VerifyFailed, name+": "+n+" is empty", nil)
		}
		out.Size += info.Size()
	}

	out.Playlist = pl
	return out, nil
}

// check — an output has the planned frame, the planned duration (within a
// tenth of a second and a frame) and audio exactly when planned.
func (a *Tools) check(out Output, width, height int, duration float64, sound bool, name string) *code.Error {
	m := out.Media

	// a tenth of a second and a frame
	tolerance := 0.1
	if m.FPS > 0 {
		tolerance += 1 / m.FPS
	}

	// the frame, the duration, the audio
	switch {
	case !m.HasVideo:
		return code.New(code.VerifyFailed, name+" has no video", nil)
	case m.Width != width:
		return code.New(code.VerifyFailed, fmt.Sprintf("%s is %dx%d, planned %dx%d", name, m.Width, m.Height, width, height), nil)
	case m.Height != height:
		return code.New(code.VerifyFailed, fmt.Sprintf("%s is %dx%d, planned %dx%d", name, m.Width, m.Height, width, height), nil)
	case math.Abs(m.Duration-duration) > tolerance:
		return code.New(code.VerifyFailed, fmt.Sprintf("%s lasts %.3fs, planned %.3fs", name, m.Duration, duration), nil)
	case m.HasAudio != sound:
		return code.New(code.VerifyFailed, fmt.Sprintf("%s has audio %v, planned %v", name, m.HasAudio, sound), nil)
	}
	return nil
}

// aligned — every rendition is cut at the same points: as many segments
// as the first one, each as long within a millisecond (the playlists
// print EXTINF to the microsecond); the CODECS of each is known.
func (a *Tools) aligned(p *Plan, outs []Output) *code.Error {
	// the first rendition is the reference
	first := outs[0].Playlist
	for i, out := range outs {
		name := p.Renditions[i].Profile.Name

		// known codecs and as many segments as the first
		switch {
		case out.Media.Codecs == "":
			return code.New(code.VerifyFailed, name+": the codecs of the rendition are unknown", nil)
		case len(out.Playlist.Segments) != len(first.Segments):
			return code.New(code.VerifyFailed, fmt.Sprintf("%s has %d segments, %s has %d", name, len(out.Playlist.Segments), p.Renditions[0].Profile.Name, len(first.Segments)), nil)
		}

		// each segment as long as the first one's
		for j, s := range out.Playlist.Segments {
			if math.Abs(s.Duration-first.Segments[j].Duration) > 0.001 {
				return code.New(code.VerifyFailed, fmt.Sprintf("%s segment %d lasts %.6fs, %s %.6fs", name, j, s.Duration, p.Renditions[0].Profile.Name, first.Segments[j].Duration), nil)
			}
		}
	}
	return nil
}

// bytes — the bytes of the video and of the audio packets of a file.
func (a *Tools) bytes(ctx context.Context, file string) (video, audio int64, err error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return 0, 0, err
	}

	out, _, err := a.run(ctx, "", a.FFprobe, "-v", "error", "-protocol_whitelist", "file",
		"-show_entries", "packet=codec_type,size", "-of", "csv=p=0", "file:"+abs)
	if err != nil {
		return 0, 0, err
	}

	// one "video,1234" line per packet (a packet with side data has more
	// fields after the size)
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ",")
		if len(fields) < 2 {
			continue
		}

		// the kind and the size of the packet
		kind := fields[0]
		size, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("video: packet size %q", fields[1])
		}

		// added to its stream
		switch kind {
		case "video":
			video += size
		case "audio":
			audio += size
		}
	}
	return video, audio, sc.Err()
}

// fail — a failed read back: the finished job context as itself, anything
// else VERIFY_FAILED.
func (a *Tools) fail(ctx context.Context, message string, err error) *code.Error {
	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	return code.New(code.VerifyFailed, message, err)
}

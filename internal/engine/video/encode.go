package video

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	job "github.com/angels-you/bee/pkg/types/video"
)

// Encode — the one ffmpeg run of the plan: the source decoded once, every
// output written under dir at its path (the HLS renditions, or the MP4,
// the preview, the thumbnails). dir must exist; the rendition folders are
// made here. The tools see only local files.
func (a *Tools) Encode(ctx context.Context, p *Plan, dir string) error {
	// the rendition folders (HLS only)
	for _, r := range p.Renditions {
		if r.Dir == "" {
			continue
		}

		err := os.MkdirAll(filepath.Join(dir, r.Dir), 0o755)
		if err != nil {
			return err
		}
	}

	// the arguments of the run
	args, err := a.args(p)
	if err != nil {
		return err
	}

	// the one ffmpeg run
	_, _, err = a.run(ctx, dir, a.FFmpeg, args...)
	return err
}

// args — the arguments of the run: the inputs, the graph, then every
// output; output paths are relative to the directory of the run.
func (a *Tools) args(p *Plan) ([]string, error) {
	// the global flags and the threads of every encoder
	args := []string{"-nostdin", "-hide_banner", "-nostats", "-loglevel", "error", "-y"}
	threads := strconv.Itoa(a.Threads)

	// the inputs, the watermark picture included
	inputs, err := a.inputs(p, true)
	if err != nil {
		return nil, err
	}
	args = append(args, inputs...)

	// the graph
	args = append(args, "-filter_complex", p.graph(), "-filter_complex_threads", threads)

	// the renditions
	for i, r := range p.Renditions {
		// the streams of the rendition, no metadata or chapters of the source
		args = append(args, "-map", fmt.Sprintf("[v%d]", i))
		if p.HasSound {
			args = append(args, "-map", fmt.Sprintf("[a%d]", i))
		}
		args = append(args, "-map_metadata", "-1", "-map_chapters", "-1")

		// the encoders
		args = append(args, a.videoArgs(p, r.Profile, p.keyint(r.Profile))...)
		if p.HasSound {
			args = append(args, a.soundArgs(r.Profile)...)
		}
		args = append(args, "-threads", threads)

		// a single MP4
		if p.HLS == nil {
			args = append(args, "-movflags", "+faststart", "-f", "mp4", r.File)
			continue
		}

		// an HLS rendition
		args = append(args, a.hlsArgs(p.HLS, r)...)
	}

	// the preview: MP4, silent unless it asks for sound
	if p.Clip != nil {
		profile := p.Clip.Preview.Profile
		sound := p.HasSound && p.Clip.Preview.Sound

		// the streams of the preview
		args = append(args, "-map", "[pv]")
		if sound {
			args = append(args, "-map", "[pa]")
		}
		args = append(args, "-map_metadata", "-1", "-map_chapters", "-1")

		// the encoders and the muxer
		args = append(args, a.videoArgs(p, profile, 0)...)
		if sound {
			args = append(args, a.soundArgs(profile)...)
		}
		args = append(args, "-threads", threads, "-movflags", "+faststart", "-f", "mp4", p.Clip.Path)
	}

	// the thumbnails: one frame each
	for i, s := range p.Stills {
		args = append(args, "-map", fmt.Sprintf("[t%d]", i), "-map_metadata", "-1", "-frames:v", "1")
		args = append(args, a.stillArgs(s)...)
		args = append(args, s.Path)
	}
	return args, nil
}

// inputs — the input arguments of a run, shared by the encode and the
// loudness pass so both read exactly the same timeline: input 0 the
// source trimmed on read (an accurate seek: decoded from the keyframe
// before Start, the frames before Start dropped); input 1 the external
// sound from SoundStart, looped by the demuxer (each loop from the start
// of the file); then the watermark picture when picture is true. Every
// input is a local file read with its own demuxers only.
func (a *Tools) inputs(p *Plan, picture bool) ([]string, error) {
	var args []string

	// the source
	source, err := filepath.Abs(p.Source.Path)
	if err != nil {
		return nil, err
	}
	if p.Start > 0 {
		args = append(args, "-ss", p.num(p.Start))
	}
	args = append(args, "-t", p.num(p.Length), "-protocol_whitelist", "file", "-format_whitelist", a.sourceFormats, "-i", "file:"+source)

	// the external sound
	if p.external {
		sound, err := filepath.Abs(p.Sound.Path)
		if err != nil {
			return nil, err
		}

		// looped and started at SoundStart
		if p.Options.SoundLoop {
			args = append(args, "-stream_loop", "-1")
		}
		if p.Options.SoundStart > 0 {
			args = append(args, "-ss", p.num(p.Options.SoundStart))
		}
		args = append(args, "-protocol_whitelist", "file", "-format_whitelist", a.soundFormats, "-i", "file:"+sound)
	}

	// the watermark picture
	if picture && p.Mark != nil {
		mark, err := filepath.Abs(p.Watermark.Path)
		if err != nil {
			return nil, err
		}
		args = append(args, "-protocol_whitelist", "file", "-format_whitelist", a.pictureFormats, "-i", "file:"+mark)
	}
	return args, nil
}

// videoArgs — the encoder of a profile: codec, preset, rate control and,
// for HLS (keyint > 0), a keyframe every keyint frames, closed GOPs and no
// scene-cut keyframes, so every segment of every rendition starts with a
// keyframe at the same time. A tone mapped source is tagged bt709.
func (a *Tools) videoArgs(p *Plan, profile job.Profile, keyint int) []string {
	// the codec and its preset
	args := []string{"-c:v", a.encoder(profile.VideoCodec)}
	if profile.Preset != "" {
		args = append(args, "-preset", profile.Preset)
	}

	// rate control: target bitrate or quality, optionally capped
	switch profile.RateControl {
	case job.RateVBR:
		args = append(args, "-b:v", strconv.Itoa(profile.VideoBitrate)+"k")
	case job.RateCRF:
		args = append(args, "-crf", strconv.Itoa(profile.CRF))
	}
	if profile.MaxBitrate > 0 {
		args = append(args, "-maxrate", strconv.Itoa(profile.MaxBitrate)+"k", "-bufsize", strconv.Itoa(profile.BufferSize)+"k")
	}

	// GOP and the codec specifics
	gop := strconv.Itoa(keyint)
	switch profile.VideoCodec {
	case job.VideoH264:
		if keyint > 0 {
			args = append(args, "-g", gop, "-keyint_min", gop, "-sc_threshold", "0", "-flags", "+cgop")
		}
	case job.VideoHEVC:
		params := "log-level=error"
		if keyint > 0 {
			params += ":keyint=" + gop + ":min-keyint=" + gop + ":scenecut=0:open-gop=0"
		}
		args = append(args, "-tag:v", "hvc1", "-x265-params", params)
	case job.VideoAV1:
		if keyint > 0 {
			args = append(args, "-g", gop)
		}
	}

	// a tone mapped source is tagged bt709
	if p.Tonemap {
		args = append(args, "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709")
	}
	return args
}

// soundArgs — AAC-LC at the bitrate of the profile.
func (a *Tools) soundArgs(profile job.Profile) []string {
	return []string{"-c:a", "aac", "-profile:a", "aac_low", "-b:a", strconv.Itoa(profile.SoundBitrate) + "k"}
}

// hlsArgs — the HLS VOD muxer of a rendition: its folder holds the media
// playlist, init.mp4 (fMP4) and segment_NNNNN.m4s or .ts; the playlist
// names them relative to itself.
func (a *Tools) hlsArgs(h *job.HLS, r Rendition) []string {
	// a VOD playlist of independent segments
	args := []string{
		"-f", "hls",
		"-hls_time", strconv.FormatFloat(h.SegmentDuration, 'f', -1, 64),
		"-hls_playlist_type", "vod",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments",
	}

	// the segments: fMP4 with an init, or MPEG-TS
	switch h.Format {
	case job.HLSFMP4:
		args = append(args, "-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_filename", filepath.Join(r.Dir, "segment_%05d.m4s"))
	case job.HLSMPEGTS:
		args = append(args, "-hls_segment_type", "mpegts",
			"-hls_segment_filename", filepath.Join(r.Dir, "segment_%05d.ts"))
	}

	// the media playlist
	return append(args, r.File)
}

// keyint — the frames between keyframes of an HLS rendition: Keyframe
// seconds at the profile rate (bee validates it is a whole number);
// 0 for a single MP4, which keeps the encoder's own GOP.
func (a *Plan) keyint(profile job.Profile) int {
	if a.HLS == nil {
		return 0
	}

	return int(math.Round(float64(profile.FPS) * a.HLS.Keyframe))
}

package video

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angels-you/bee/internal/testmedia/testvideo"
	"github.com/angels-you/bee/pkg/types/code"
	job "github.com/angels-you/bee/pkg/types/video"
)

// suite — the tools of a test (checked against the local ffmpeg), its
// directories and its media maker.
type suite struct {
	t     *testing.T
	ctx   context.Context
	tools *Tools
	in    string // generated sources
	out   string // the outputs of the run
	make  testvideo.Maker
}

// init — the suite of t; skips when ffmpeg or ffprobe is not installed.
func (a *suite) init(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		_, err := exec.LookPath(bin)
		if err != nil {
			t.Skip(bin + " is not installed")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	a.t = t
	a.ctx = ctx
	a.in = t.TempDir()
	a.out = t.TempDir()
	a.make = testvideo.Maker{T: t, Dir: a.in}
	a.tools = &Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Threads: 2}

	err := a.tools.Check(ctx)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
}

// options — job options with every default filled as bee fills them:
// contain, centre, black, speed 1, volumes 1.
func (a *suite) options() job.Options {
	one := 1.0
	return job.Options{
		Resize:         job.ResizeContain,
		Gravity:        job.GravityCenter,
		Background:     "#000000",
		Speed:          1,
		SoundVolume:    &one,
		OriginalVolume: &one,
	}
}

// profile — an H.264 + AAC profile of w × h at fps as bee fills it.
func (a *suite) profile(name string, w, h, fps int) job.Profile {
	return job.Profile{
		Name:         name,
		Width:        w,
		Height:       h,
		FPS:          fps,
		VideoCodec:   job.VideoH264,
		PixelFormat:  "yuv420p",
		Preset:       "ultrafast",
		RateControl:  job.RateVBR,
		VideoBitrate: 300,
		SoundCodec:   job.SoundAAC,
		SoundBitrate: 64,
		SampleRate:   48000,
		Channels:     2,
	}
}

// file — a generated file probed as what it is: a source, a sound or a
// picture (each kind with its own demuxers).
func (a *suite) file(path string) *File {
	a.t.Helper()
	var m Media
	var err error
	switch filepath.Ext(path) {
	case ".png":
		m, err = a.tools.ProbePicture(a.ctx, path)
	case ".m4a":
		m, err = a.tools.ProbeSound(a.ctx, path)
	default:
		m, err = a.tools.ProbeSource(a.ctx, path)
	}
	if err != nil {
		a.t.Fatalf("probe %s: %v", path, err)
	}
	return &File{Path: path, Media: m}
}

// run — plan, encode and verify in; fails the test on any error.
func (a *suite) run(in Input) (*Plan, *Report) {
	a.t.Helper()
	p, e := a.tools.Plan(in)
	if e != nil {
		a.t.Fatalf("plan: %v", e)
	}

	err := a.tools.Normalize(a.ctx, p)
	if err != nil {
		a.t.Fatalf("normalize: %v", err)
	}
	err = a.tools.Encode(a.ctx, p, a.out)
	if err != nil {
		a.t.Fatalf("encode: %v\ngraph: %s", err, p.graph())
	}

	report, e := a.tools.Verify(a.ctx, p, a.out)
	if e != nil {
		a.t.Fatalf("verify: %v\ngraph: %s", e, p.graph())
	}
	return p, report
}

// keyframes — the times of the keyframes of a file, seconds.
func (a *suite) keyframes(file string) []float64 {
	a.t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-skip_frame", "nokey",
		"-show_entries", "frame=pts_time", "-of", "csv=p=0", file).Output()
	if err != nil {
		a.t.Fatalf("keyframes of %s: %v", file, err)
	}

	var times []float64
	for _, line := range strings.Fields(string(out)) {
		v, err := strconv.ParseFloat(strings.TrimSuffix(line, ","), 64)
		if err != nil {
			a.t.Fatalf("keyframe time %q", line)
		}
		times = append(times, v)
	}
	return times
}

// TestProbe — the fields of a phone-like source: rotation, anamorphic
// pixels, audio, the codec strings; HDR tags.
func TestProbe(t *testing.T) {
	s := suite{}
	s.init(t)

	// a portrait phone video: coded landscape, a display matrix of -90°
	src := s.file(s.make.Clip(testvideo.Clip{Name: "phone.mp4", Width: 320, Height: 180, FPS: 30, Seconds: 1, Audio: true, Rotation: -90}))
	m := src.Media
	switch {
	case m.Width != 320 || m.Height != 180:
		t.Errorf("coded %dx%d", m.Width, m.Height)
	case m.Rotation != 90:
		t.Errorf("rotation %d", m.Rotation)
	case m.DisplayWidth != 180 || m.DisplayHeight != 320:
		t.Errorf("display %dx%d", m.DisplayWidth, m.DisplayHeight)
	case m.FPS != 30 || m.VFR:
		t.Errorf("fps %v vfr %v", m.FPS, m.VFR)
	case !m.HasAudio || m.SampleRate != 48000 || m.Channels != 2 || m.AudioCodec != "aac":
		t.Errorf("audio %+v", m)
	case !strings.HasPrefix(m.Codecs, "avc1.") || !strings.HasSuffix(m.Codecs, ",mp4a.40.2"):
		t.Errorf("codecs %q", m.Codecs)
	case math.Abs(m.Duration-1) > 0.05:
		t.Errorf("duration %v", m.Duration)
	}

	// the other way round
	src = s.file(s.make.Clip(testvideo.Clip{Name: "ccw.mp4", Width: 320, Height: 180, FPS: 30, Seconds: 1, Rotation: 90}))
	if src.Media.Rotation != 270 || src.Media.DisplayWidth != 180 || src.Media.HasAudio {
		t.Errorf("rotation 90: %+v", src.Media)
	}

	// anamorphic 2:1 pixels are shown twice as wide
	src = s.file(s.make.Clip(testvideo.Clip{Name: "wide.mp4", Width: 320, Height: 180, FPS: 25, Seconds: 1, SAR: "2:1"}))
	if src.Media.SAR != "2:1" || src.Media.DisplayWidth != 640 || src.Media.DisplayHeight != 180 {
		t.Errorf("anamorphic %+v", src.Media)
	}

	// HDR: 10-bit HEVC tagged PQ
	src = s.file(s.make.Clip(testvideo.Clip{Name: "hdr.mp4", Width: 320, Height: 180, FPS: 30, Seconds: 1, HDR: true}))
	switch {
	case !src.Media.HDR:
		t.Errorf("hdr %+v", src.Media)
	case src.Media.PixelFormat != "yuv420p10le":
		t.Errorf("pixel format %s", src.Media.PixelFormat)
	case !strings.HasPrefix(src.Media.Codecs, "hvc1.2.4.L"):
		t.Errorf("hevc main 10 codecs %q", src.Media.Codecs)
	}

	// not a media file
	junk := filepath.Join(s.in, "junk.mp4")
	err := os.WriteFile(junk, []byte("not a video"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.tools.ProbeSource(s.ctx, junk)
	if err == nil {
		t.Error("junk probed")
	}

	// a playlist posing as a video is not read: no hls demuxer for inputs
	posing := filepath.Join(s.in, "playlist.mp4")
	err = os.WriteFile(posing, []byte("#EXTM3U\n#EXTINF:1,\nfile:"+filepath.Join(s.in, "ccw.mp4")+"\n#EXT-X-ENDLIST\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.tools.ProbeSource(s.ctx, posing)
	if err == nil {
		t.Error("a playlist read as a source")
	}

	// a picture is not a source, a video is not a picture
	_, err = s.tools.ProbeSource(s.ctx, s.make.Picture(testvideo.Picture{Name: "mark.png", Width: 16, Height: 16}))
	if err == nil {
		t.Error("a png read as a source")
	}
	_, err = s.tools.ProbePicture(s.ctx, filepath.Join(s.in, "ccw.mp4"))
	if err == nil {
		t.Error("a video read as a picture")
	}
}

// TestStream — a landscape source with audio covered into two vertical
// renditions of different rates, with a watermark, a preview and
// thumbnails: one run, every output read back, the keyframes and the
// segments of the renditions at the same times.
func TestStream(t *testing.T) {
	s := suite{}
	s.init(t)

	src := s.file(s.make.Clip(testvideo.Clip{Name: "source.mp4", Width: 640, Height: 360, FPS: 30, Seconds: 4, Audio: true}))
	mark := s.file(s.make.Picture(testvideo.Picture{Name: "mark.png", Width: 64, Height: 32}))

	o := s.options()
	o.Vertical, o.Resize = true, job.ResizeCover
	opacity := 0.5
	o.Watermark = &job.Watermark{Position: job.GravityBottomRight, Width: 40, Margin: 4, Opacity: &opacity}
	hls := job.HLS{Format: job.HLSFMP4, SegmentDuration: 1, Keyframe: 1}

	p, report := s.run(Input{
		Source:    *src,
		Watermark: mark,
		Options:   o,
		Profiles:  []job.Profile{s.profile("low", 180, 320, 30), s.profile("high", 202, 360, 60)},
		HLS:       &hls,
		Thumbnails: []job.Thumbnail{
			{Output: "a.jpg", Position: 1, Format: job.ImageJPEG, Quality: 80},
			{Output: "b.png", Position: 99, Width: 90, Format: job.ImagePNG},
		},
		Preview: &job.Preview{Output: "p.mp4", Start: 1, Duration: 2, Profile: s.profile("preview", 180, 320, 15)},
	})

	// the renditions
	for i, out := range report.Renditions {
		r := p.Renditions[i]
		m := out.Media
		switch {
		case m.Width != r.Profile.Width || m.Height != r.Profile.Height:
			t.Errorf("%s: %dx%d", r.Profile.Name, m.Width, m.Height)
		case m.FPS != float64(r.Profile.FPS):
			t.Errorf("%s: fps %v", r.Profile.Name, m.FPS)
		case !m.HasAudio:
			t.Errorf("%s: no audio", r.Profile.Name)
		case len(out.Playlist.Segments) != 4:
			t.Errorf("%s: %d segments", r.Profile.Name, len(out.Playlist.Segments))
		case out.Playlist.Init != "init.mp4" || !out.Playlist.Independent || out.Playlist.Version != 7:
			t.Errorf("%s: playlist %+v", r.Profile.Name, out.Playlist)
		case m.VideoBitrate <= 0 || m.AudioBitrate <= 0:
			t.Errorf("%s: bitrates %d %d", r.Profile.Name, m.VideoBitrate, m.AudioBitrate)
		}
	}

	// keyframes every second in both, at the same times (the timeline
	// starts after the AAC priming, so the times are relative to the first)
	low := s.keyframes(filepath.Join(s.out, p.Renditions[0].File))
	high := s.keyframes(filepath.Join(s.out, p.Renditions[1].File))
	if len(low) != 4 || len(high) != 4 {
		t.Fatalf("keyframes %v %v", low, high)
	}
	for i := range low {
		if math.Abs(low[i]-low[0]-float64(i)) > 0.001 {
			t.Errorf("low keyframe %d at %v", i, low[i])
		}
		if math.Abs(high[i]-low[i]) > 0.001 {
			t.Errorf("keyframe %d at %v and %v", i, low[i], high[i])
		}
	}

	// the extras
	switch {
	case report.Preview.Media.HasAudio:
		t.Error("the preview has audio")
	case report.Preview.Media.FPS != 15 || math.Abs(report.Preview.Media.Duration-2) > 0.1:
		t.Errorf("preview %+v", report.Preview.Media)
	case report.Stills[0].Width != 202 || report.Stills[0].Height != 360:
		t.Errorf("thumbnail 0 %+v", report.Stills[0])
	case report.Stills[1].Width != 90 || report.Stills[1].Height != 160:
		t.Errorf("thumbnail 1 %+v", report.Stills[1])
	}
}

// TestVideo — a portrait phone source into one faststart MP4: the display
// matrix applied and gone, trimmed and sped up, the original audio at the
// speed, the source frame rate kept.
func TestVideo(t *testing.T) {
	s := suite{}
	s.init(t)

	src := s.file(s.make.Clip(testvideo.Clip{Name: "phone.mp4", Width: 640, Height: 360, FPS: 25, Seconds: 4, Audio: true, Rotation: -90}))
	o := s.options()
	o.Vertical, o.Resize = true, job.ResizeCover
	o.Start, o.Duration, o.Speed = 1, 2, 2
	profile := s.profile("", 360, 640, 0)

	_, report := s.run(Input{Source: *src, Options: o, Profiles: []job.Profile{profile}})
	m := report.Renditions[0].Media
	switch {
	case m.Rotation != 0:
		t.Errorf("rotation %d left in the output", m.Rotation)
	case m.Width != 360 || m.Height != 640:
		t.Errorf("frame %dx%d", m.Width, m.Height)
	case math.Abs(m.Duration-1) > 0.1:
		t.Errorf("duration %v, want 1", m.Duration)
	case m.FPS != 25:
		t.Errorf("fps %v: fps 0 keeps the source rate through the speed", m.FPS)
	case !m.HasAudio || m.SampleRate != 48000:
		t.Errorf("audio %+v", m)
	}

	// faststart: the index before the media
	data, err := os.ReadFile(filepath.Join(s.out, "video.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	moov := bytes.Index(data, []byte("moov"))
	mdat := bytes.Index(data, []byte("mdat"))
	if moov < 0 || mdat < 0 || moov > mdat {
		t.Errorf("moov at %d, mdat at %d: not faststart", moov, mdat)
	}
}

// TestSound — the external sound replaces or joins the original, looped
// or padded with silence; the output is always as long as the video; no
// audio at all gives an output without an audio stream.
func TestSound(t *testing.T) {
	s := suite{}
	s.init(t)

	silent := s.file(s.make.Clip(testvideo.Clip{Name: "silent.mp4", Width: 320, Height: 180, FPS: 30, Seconds: 3}))
	loud := s.file(s.make.Clip(testvideo.Clip{Name: "loud.mp4", Width: 320, Height: 180, FPS: 30, Seconds: 3, Audio: true}))
	sound := s.file(s.make.Sound(testvideo.Sound{Name: "sound.m4a", Seconds: 1.2, Freq: 880}))
	profile := s.profile("", 180, 320, 30)

	// a silent source: no audio stream
	_, report := s.run(Input{Source: *silent, Options: s.options(), Profiles: []job.Profile{profile}})
	if report.Renditions[0].Media.HasAudio {
		t.Error("audio from nothing")
	}

	// a short sound looped under a silent source, normalized and faded
	o := s.options()
	o.SoundLoop, o.Normalize, o.FadeIn, o.FadeOut = true, true, 0.5, 0.5
	preview := &job.Preview{Output: "p.mp4", Start: 2, Duration: 3, Sound: true, Profile: s.profile("preview", 180, 320, 30)}
	_, report = s.run(Input{Source: *silent, Sound: sound, Options: o, Profiles: []job.Profile{profile}, Preview: preview, LUFS: -16, TruePeak: -1.5})
	m := report.Renditions[0].Media
	if !m.HasAudio || math.Abs(m.Duration-3) > 0.1 {
		t.Errorf("looped sound: audio %v duration %v", m.HasAudio, m.Duration)
	}
	if !report.Preview.Media.HasAudio || math.Abs(report.Preview.Media.Duration-1) > 0.1 {
		t.Errorf("preview with sound: audio %v duration %v", report.Preview.Media.HasAudio, report.Preview.Media.Duration)
	}

	// the sound from its offset, not looped, mixed with the original
	o = s.options()
	o.SoundStart = 0.5
	_, report = s.run(Input{Source: *loud, Sound: sound, Options: o, Profiles: []job.Profile{profile}})
	m = report.Renditions[0].Media
	if !m.HasAudio || math.Abs(m.Duration-3) > 0.1 {
		t.Errorf("mixed sound: audio %v duration %v", m.HasAudio, m.Duration)
	}

	// NoSound wins over everything
	o = s.options()
	o.NoSound = true
	_, report = s.run(Input{Source: *loud, Options: o, Profiles: []job.Profile{profile}})
	if report.Renditions[0].Media.HasAudio {
		t.Error("NoSound kept the audio")
	}
}

// TestCodecs — HEVC and AV1 renditions in fMP4 and H.264 in MPEG-TS, each
// with keyframes on the segment boundaries and its CODECS string.
func TestCodecs(t *testing.T) {
	s := suite{}
	s.init(t)
	src := s.file(s.make.Clip(testvideo.Clip{Name: "source.mp4", Width: 320, Height: 568, FPS: 30, Seconds: 2, Audio: true}))

	// HEVC and AV1 when the build has them
	for _, c := range []job.VideoCodec{job.VideoHEVC, job.VideoAV1} {
		if !s.tools.CanEncode(c) {
			t.Logf("ffmpeg has no %s encoder", c)
			continue
		}
		profile := s.profile("r", 180, 320, 30)
		profile.VideoCodec, profile.Preset = c, ""
		if c == job.VideoAV1 {
			profile.Preset = "12"
		}
		hls := job.HLS{Format: job.HLSFMP4, SegmentDuration: 1, Keyframe: 1}
		s.out = t.TempDir()
		second := profile
		second.Name = "r2"
		p, report := s.run(Input{Source: *src, Options: s.options(), Profiles: []job.Profile{profile, second}, HLS: &hls})

		codecs := report.Renditions[0].Media.Codecs
		prefix := map[job.VideoCodec]string{job.VideoHEVC: "hvc1.1.6.L", job.VideoAV1: "av01.0."}[c]
		if !strings.HasPrefix(codecs, prefix) || !strings.HasSuffix(codecs, ",mp4a.40.2") {
			t.Errorf("%s codecs %q", c, codecs)
		}
		keys := s.keyframes(filepath.Join(s.out, p.Renditions[1].File))
		if len(keys) != 2 || math.Abs(keys[1]-keys[0]-1) > 0.001 {
			t.Errorf("%s keyframes %v", c, keys)
		}
	}

	// MPEG-TS: no init, the codecs from the SPS
	hls := job.HLS{Format: job.HLSMPEGTS, SegmentDuration: 1, Keyframe: 1}
	s.out = t.TempDir()
	profile := s.profile("ts", 180, 320, 30)
	profile.Preset = "fast"
	_, report := s.run(Input{Source: *src, Options: s.options(), Profiles: []job.Profile{profile}, HLS: &hls})
	out := report.Renditions[0]
	switch {
	case out.Playlist.Init != "":
		t.Errorf("ts init %q", out.Playlist.Init)
	case !strings.HasSuffix(out.Playlist.Segments[0].Name, ".ts"):
		t.Errorf("ts segment %s", out.Playlist.Segments[0].Name)
	case !strings.HasPrefix(out.Media.Codecs, "avc1.64"):
		t.Errorf("ts high profile codecs %q", out.Media.Codecs)
	}
}

// TestStills — every thumbnail format the build writes.
func TestStills(t *testing.T) {
	s := suite{}
	s.init(t)
	src := s.file(s.make.Clip(testvideo.Clip{Name: "source.mp4", Width: 320, Height: 568, FPS: 30, Seconds: 1}))

	var thumbnails []job.Thumbnail
	for _, f := range []job.ImageFormat{job.ImageJPEG, job.ImagePNG, job.ImageWebP, job.ImageAVIF} {
		if !s.tools.CanWrite(f) {
			t.Logf("ffmpeg cannot write %s", f)
			continue
		}
		thumbnails = append(thumbnails, job.Thumbnail{Output: "t." + string(f), Format: f, Quality: 70, Width: 128, Position: 0.5})
	}
	p, report := s.run(Input{Source: *src, Options: s.options(), Profiles: []job.Profile{s.profile("", 180, 320, 0)}, Thumbnails: thumbnails})

	for i, img := range report.Stills {
		switch {
		case img.Width != 128 || img.Height != 228:
			t.Errorf("%s: %dx%d", p.Stills[i].Path, img.Width, img.Height)
		case img.Size == 0:
			t.Errorf("%s: empty", p.Stills[i].Path)
		}
	}
}

// TestHDR — an HDR source is tone mapped to bt709 SDR when the build
// can; otherwise the plan refuses it.
func TestHDR(t *testing.T) {
	s := suite{}
	s.init(t)
	src := s.file(s.make.Clip(testvideo.Clip{Name: "hdr.mp4", Width: 320, Height: 568, FPS: 30, Seconds: 1, HDR: true}))
	in := Input{Source: *src, Options: s.options(), Profiles: []job.Profile{s.profile("", 180, 320, 30)}}

	if !s.tools.CanTonemap() {
		_, e := s.tools.Plan(in)
		if e == nil || e.Code != code.UnsupportedFormat {
			t.Errorf("HDR without zscale: %v", e)
		}
		t.Log("ffmpeg has no zscale: tone mapping not run")
		return
	}

	_, report := s.run(in)
	m := report.Renditions[0].Media
	if m.HDR || m.ColorTransfer != "bt709" || m.PixelFormat != "yuv420p" {
		t.Errorf("tone mapped %+v", m)
	}
}

// loudnessOf — the loudness of the first audio stream of a file.
func (a *suite) loudnessOf(file string) Loudness {
	a.t.Helper()
	abs, err := filepath.Abs(file)
	if err != nil {
		a.t.Fatal(err)
	}
	l, err := a.tools.measure(a.ctx, []string{"-i", "file:" + abs}, nil, "[0:a:0]")
	if err != nil {
		a.t.Fatalf("measure %s: %v", file, err)
	}
	return l
}

// normalized — a silent 6 second source with s as its sound, normalized
// to -16 LUFS under -1.5 dBTP into an MP4; the plan and the loudness of
// the MP4.
func (a *suite) normalized(s testvideo.Sound) (*Plan, Loudness) {
	a.t.Helper()
	src := a.file(a.make.Clip(testvideo.Clip{Name: "silent.mp4", Width: 160, Height: 284, FPS: 15, Seconds: s.Seconds}))
	sound := a.file(a.make.Sound(s))
	o := a.options()
	o.Normalize = true
	a.out = a.t.TempDir()

	p, _ := a.run(Input{Source: *src, Sound: sound, Options: o, Profiles: []job.Profile{a.profile("", 160, 284, 15)}, LUFS: -16, TruePeak: -1.5})
	return p, a.loudnessOf(filepath.Join(a.out, "video.mp4"))
}

// TestNormalize — linear gain: a quiet and a loud sound both land at the
// target, a peaky one is held under the true peak ceiling (and stays
// below the target), the loudness range (the dynamics) is kept.
func TestNormalize(t *testing.T) {
	s := suite{}
	s.init(t)

	// quiet: raised
	p, out := s.normalized(testvideo.Sound{Name: "quiet.m4a", Seconds: 6, Freq: 440, VolumeDB: -40})
	t.Logf("quiet: mix %+v gain %v → output %+v", *p.Loudness, p.Gain, out)
	if p.Gain < 20 || math.Abs(out.Integrated+16) > 1 {
		t.Errorf("quiet: gain %v, output %.2f LUFS", p.Gain, out.Integrated)
	}

	// loud: lowered
	p, out = s.normalized(testvideo.Sound{Name: "loud.m4a", Seconds: 6, Freq: 440, VolumeDB: -1})
	t.Logf("loud: mix %+v gain %v → output %+v", *p.Loudness, p.Gain, out)
	if p.Gain > -5 || math.Abs(out.Integrated+16) > 1 {
		t.Errorf("loud: gain %v, output %.2f LUFS", p.Gain, out.Integrated)
	}

	// peaky: the gain stops at the true peak ceiling
	p, out = s.normalized(testvideo.Sound{Name: "peaky.m4a", Seconds: 6, Freq: 440, VolumeDB: -40, Clicks: true})
	t.Logf("peaky: mix %+v gain %v → output %+v", *p.Loudness, p.Gain, out)
	switch {
	case p.Loudness.TruePeak+p.Gain > -1.5+0.01:
		t.Errorf("peaky: true peak %.2f + gain %.2f passes the ceiling", p.Loudness.TruePeak, p.Gain)
	case out.TruePeak > -1.5+0.5:
		t.Errorf("peaky: output true peak %.2f", out.TruePeak)
	case out.Integrated > -16-3:
		t.Errorf("peaky: output %.2f LUFS reached the target: the gain was not limited", out.Integrated)
	}

	// dynamics: the loudness range of the sound is kept
	in := s.loudnessOf(s.make.Sound(testvideo.Sound{Name: "step.m4a", Seconds: 8, Freq: 440, VolumeDB: -20, Step: true}))
	p, out = s.normalized(testvideo.Sound{Name: "step.m4a", Seconds: 8, Freq: 440, VolumeDB: -20, Step: true})
	t.Logf("step: input %+v gain %v → output %+v", in, p.Gain, out)
	if in.LRA < 10 || math.Abs(out.LRA-in.LRA) > 1 {
		t.Errorf("dynamics: input LRA %.1f, output LRA %.1f (gain %v)", in.LRA, out.LRA, p.Gain)
	}
}

// TestNormalizeOff — without Normalize there is no loudness pass (a
// missing ffmpeg is never run) and no gain in the graph.
func TestNormalizeOff(t *testing.T) {
	f := planFixture{}
	f.init(t)
	p := f.plan(f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960)))
	f.tools.FFmpeg = "/nonexistent/ffmpeg"

	err := f.tools.Normalize(context.Background(), p)
	switch {
	case err != nil:
		t.Errorf("a pass ran: %v", err)
	case p.Loudness != nil:
		t.Error("measured without Normalize")
	case strings.Contains(p.graph(), "dB,"):
		t.Errorf("a gain without Normalize: %s", p.graph())
	}

	// with Normalize it runs (and fails here: no ffmpeg)
	p.Options.Normalize = true
	err = f.tools.Normalize(context.Background(), p)
	if err == nil {
		t.Error("no pass with Normalize")
	}
}

// TestLoudnessGain — the gain rule of Bee: to the target, held under the
// ceiling, 0 for silence.
func TestLoudnessGain(t *testing.T) {
	cases := map[Loudness]float64{
		{Integrated: -30, TruePeak: -20}:  14,
		{Integrated: -10, TruePeak: -1}:   -6,
		{Integrated: -30, TruePeak: -2}:   0.5,
		{Integrated: -70, TruePeak: -144}: 0,
	}
	for l, want := range cases {
		got := l.gain(-16, -1.5)
		if got != want {
			t.Errorf("%+v: gain %v, want %v", l, got, want)
		}
	}
}

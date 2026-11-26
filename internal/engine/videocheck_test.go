package engine

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/angels-you/bee/internal/config"
	"github.com/angels-you/bee/pkg/types/code"
	job "github.com/angels-you/bee/pkg/types/video"
)

// fixture — the config of the examples and valid jobs to change one field
// at a time.
type fixture struct {
	t   *testing.T
	cfg videoCheck
}

// init — the config with every field set as in the env examples.
func (a *fixture) init(t *testing.T) {
	a.t = t
	c := &config.Config{}
	c.Video.MaxDimension = 3840
	c.Video.MaxDuration = 5 * time.Minute
	c.Video.SoundBitrate = 128
	c.Video.WatermarkWidth = 160
	c.Video.ImageQuality = 80
	c.Video.LUFS = -16
	c.Video.TruePeak = -1.5
	a.cfg = videoCheck{cfg: c}
}

// stream — a valid Stream: the mobile ladder, every optional part set.
func (a *fixture) stream() job.StreamJob {
	return job.StreamJob{
		ID:           "job-1",
		Input:        "https://bucket.s3.example.com/in/source.mov?X-Amz-Signature=secret",
		PlaylistPath: "/video/1111/index.m3u8",
		Options:      job.VerticalOptions(),
		Profiles:     job.VerticalMobile(),
		HLS:          job.DefaultHLS(),
		Thumbnails:   []job.Thumbnail{{Output: "video/1111/cover.webp", Format: job.ImageWebP}},
		Preview:      &job.Preview{Output: "video/1111/preview.mp4", Profile: job.Profile{Width: 360, Height: 640, VideoBitrate: 400}},
	}
}

// video — a valid Video of the 720p preset.
func (a *fixture) video() job.VideoJob {
	p, _ := job.Preset(job.Quality720p)
	return job.VideoJob{
		ID:       "job-2",
		Input:    "https://bucket.s3.example.com/in/source.mov",
		FilePath: "video/1111/video.mp4",
		Options:  job.VerticalOptions(),
		Profile:  p,
	}
}

// refuse — checkStream refuses j with code c and a message holding text.
func (a *fixture) refuse(j job.StreamJob, c code.Code, text string) {
	a.t.Helper()
	e := a.cfg.checkStream(&j)
	switch {
	case e == nil:
		a.t.Errorf("accepted, want %s %q", c, text)
	case e.Code != c:
		a.t.Errorf("%s %q, want %s %q", e.Code, e.Message, c, text)
	case !strings.Contains(e.Message, text):
		a.t.Errorf("message %q, want %q", e.Message, text)
	case strings.Contains(e.Message, "secret"):
		a.t.Errorf("message %q quotes the url", e.Message)
	}
}

// refuseVideo — checkVideo refuses j with code c and a message holding
// text.
func (a *fixture) refuseVideo(j job.VideoJob, c code.Code, text string) {
	a.t.Helper()
	e := a.cfg.checkVideo(&j)
	switch {
	case e == nil:
		a.t.Errorf("accepted, want %s %q", c, text)
	case e.Code != c:
		a.t.Errorf("%s %q, want %s %q", e.Code, e.Message, c, text)
	case !strings.Contains(e.Message, text):
		a.t.Errorf("message %q, want %q", e.Message, text)
	}
}

// TestCheckStreamDefaults — the defaults of job.go and of the worker are
// filled in, the keys cleaned, the caller's job untouched.
func TestCheckStreamDefaults(t *testing.T) {
	f := fixture{}
	f.init(t)

	j := job.StreamJob{
		ID:           "job-1",
		Input:        "https://s3.example.com/in.mov",
		PlaylistPath: "/video/1/index.m3u8",
		Options:      job.Options{Sound: "https://s3.example.com/music.mp3", Watermark: &job.Watermark{Input: "https://s3.example.com/mark.png"}},
		Profiles:     []job.Profile{{Name: "360p", Width: 360, Height: 640, FPS: 30, VideoBitrate: 500}},
		Thumbnails:   []job.Thumbnail{{Output: "video/1/cover.jpg"}},
		Preview:      &job.Preview{Output: "video/1/preview.mp4", Profile: job.Profile{Width: 360, Height: 640, VideoBitrate: 400}},
	}
	caller := j

	e := f.cfg.checkStream(&j)
	if e != nil {
		t.Fatal(e)
	}

	// options
	o := j.Options
	switch {
	case o.Resize != job.ResizeContain:
		t.Errorf("resize %q", o.Resize)
	case o.Gravity != job.GravityCenter:
		t.Errorf("gravity %q", o.Gravity)
	case o.Background != "#000000":
		t.Errorf("background %q", o.Background)
	case o.Speed != 1:
		t.Errorf("speed %v", o.Speed)
	case *o.SoundVolume != 1:
		t.Errorf("sound volume %v", *o.SoundVolume)
	case *o.OriginalVolume != 0:
		t.Errorf("original volume with a sound %v", *o.OriginalVolume)
	case o.Watermark.Position != job.GravityBottomRight:
		t.Errorf("watermark position %q", o.Watermark.Position)
	case o.Watermark.Width != 160:
		t.Errorf("watermark width %d", o.Watermark.Width)
	case *o.Watermark.Opacity != 1:
		t.Errorf("watermark opacity %v", *o.Watermark.Opacity)
	}

	// profile and packaging
	p := j.Profiles[0]
	switch {
	case p.VideoCodec != job.VideoH264:
		t.Errorf("codec %q", p.VideoCodec)
	case p.PixelFormat != "yuv420p":
		t.Errorf("pixel format %q", p.PixelFormat)
	case p.RateControl != job.RateVBR:
		t.Errorf("rate control %q", p.RateControl)
	case p.SoundCodec != job.SoundAAC:
		t.Errorf("sound codec %q", p.SoundCodec)
	case p.SoundBitrate != 128:
		t.Errorf("sound bitrate %d", p.SoundBitrate)
	case p.SampleRate != 48000:
		t.Errorf("sample rate %d", p.SampleRate)
	case p.Channels != 2:
		t.Errorf("channels %d", p.Channels)
	case j.HLS.Format != job.HLSFMP4:
		t.Errorf("hls format %q", j.HLS.Format)
	case j.HLS.SegmentDuration != 2:
		t.Errorf("segment %v", j.HLS.SegmentDuration)
	case j.HLS.Keyframe != 2:
		t.Errorf("keyframe %v", j.HLS.Keyframe)
	}

	// extras and keys
	switch {
	case j.Thumbnails[0].Format != job.ImageJPEG:
		t.Errorf("thumbnail format %q", j.Thumbnails[0].Format)
	case j.Thumbnails[0].Quality != 80:
		t.Errorf("thumbnail quality %d", j.Thumbnails[0].Quality)
	case j.Preview.Duration != 3:
		t.Errorf("preview duration %v", j.Preview.Duration)
	case j.Preview.Profile.SoundBitrate != 128:
		t.Errorf("preview sound bitrate %d", j.Preview.Profile.SoundBitrate)
	case j.PlaylistPath != "video/1/index.m3u8":
		t.Errorf("output %q", j.PlaylistPath)
	}

	// the caller's job is as it was
	switch {
	case caller.Profiles[0].VideoCodec != "":
		t.Error("the caller's profiles changed")
	case caller.Thumbnails[0].Format != "":
		t.Error("the caller's thumbnails changed")
	case caller.Preview.Duration != 0:
		t.Error("the caller's preview changed")
	case caller.Options.Watermark.Width != 0:
		t.Error("the caller's watermark changed")
	}
}

// TestCheckOptionsDefaults — vertical means cover; NoSound drops the
// sound; without a sound the original plays; #rgb is expanded.
func TestCheckOptionsDefaults(t *testing.T) {
	f := fixture{}
	f.init(t)

	o := job.Options{Vertical: true, Background: "#AbC", NoSound: true, Sound: "not even a url"}
	e := f.cfg.checkOptions(&o)
	if e != nil {
		t.Fatal(e)
	}
	switch {
	case o.Resize != job.ResizeCover:
		t.Errorf("vertical resize %q", o.Resize)
	case o.Background != "#aabbcc":
		t.Errorf("background %q", o.Background)
	case o.Sound != "":
		t.Error("NoSound kept the sound")
	case *o.OriginalVolume != 1:
		t.Errorf("original volume without a sound %v", *o.OriginalVolume)
	}
}

// TestCheckStreamRefuses — one wrong field at a time.
func TestCheckStreamRefuses(t *testing.T) {
	f := fixture{}
	f.init(t)

	j := f.stream()
	e := f.cfg.checkStream(&j)
	if e != nil {
		t.Fatalf("valid stream: %v", e)
	}

	// the job and urls
	j = f.stream()
	j.ID = ""
	f.refuse(j, code.InvalidJob, "id must be")
	j = f.stream()
	j.ID = "job\n1"
	f.refuse(j, code.InvalidJob, "id must be")
	j = f.stream()
	j.Input = "http://bucket.s3.example.com/in.mov?X-Amz-Signature=secret"
	f.refuse(j, code.InvalidJob, "input must be https")
	j = f.stream()
	j.Input = "ftp://bucket.s3.example.com/in.mov?X-Amz-Signature=secret"
	f.refuse(j, code.InvalidJob, "input must be https")
	j = f.stream()
	j.Input = "/local/file"
	f.refuse(j, code.InvalidJob, "input has no host")
	j = f.stream()
	f.cfg.cfg.SourceHosts = []string{"r2.cloudflarestorage.com"}
	f.refuse(j, code.InvalidJob, "input host is not allowed")
	f.cfg.cfg.SourceHosts = nil
	j = f.stream()
	j.Options.Sound = "https://"
	f.refuse(j, code.InvalidJob, "options.sound has no host")
	j = f.stream()
	j.Options.Watermark = &job.Watermark{}
	f.refuse(j, code.InvalidJob, "options.watermark.input is required")

	// options
	j = f.stream()
	j.Options.Resize = "fill"
	f.refuse(j, code.InvalidJob, "options.resize")
	j = f.stream()
	j.Options.Gravity = "middle"
	f.refuse(j, code.InvalidJob, "options.gravity")
	j = f.stream()
	j.Options.Background = "red"
	f.refuse(j, code.InvalidJob, "options.background")
	j = f.stream()
	j.Options.Background = "#12345g"
	f.refuse(j, code.InvalidJob, "options.background")
	j = f.stream()
	j.Options.Rotation = 45
	f.refuse(j, code.InvalidJob, "options.rotation")
	j = f.stream()
	j.Options.Start = -1
	f.refuse(j, code.InvalidJob, "options.start")
	j = f.stream()
	j.Options.Speed = 8
	f.refuse(j, code.InvalidJob, "options.speed")
	j = f.stream()
	nan := math.NaN()
	j.Options.Brightness = &nan
	f.refuse(j, code.InvalidJob, "options.brightness")
	j = f.stream()
	contrast := 2.5
	j.Options.Contrast = &contrast
	f.refuse(j, code.InvalidJob, "options.contrast")
	j = f.stream()
	j.Options.Blur = 51
	f.refuse(j, code.InvalidJob, "options.blur")
	j = f.stream()
	volume := -0.5
	j.Options.SoundVolume = &volume
	f.refuse(j, code.InvalidJob, "options.sound_volume")
	j = f.stream()
	j.Options.FadeOut = 301
	f.refuse(j, code.InvalidJob, "options.fade_out")
	j = f.stream()
	opacity := 1.5
	j.Options.Watermark = &job.Watermark{Input: "https://s3.example.com/m.png", Opacity: &opacity}
	f.refuse(j, code.InvalidJob, "options.watermark.opacity")

	// profiles
	j = f.stream()
	j.Profiles = nil
	f.refuse(j, code.InvalidJob, "at least one")
	j = f.stream()
	j.Profiles[1].Name = j.Profiles[0].Name
	f.refuse(j, code.InvalidJob, "used twice")
	j = f.stream()
	j.Profiles[0].Name = "a/x"
	f.refuse(j, code.InvalidJob, "name may hold only")
	j = f.stream()
	j.Profiles[0].Name = ""
	f.refuse(j, code.InvalidJob, "name is required")
	j = f.stream()
	j.Profiles[0].Width = 361
	f.refuse(j, code.InvalidDimensions, "width must be even")
	j = f.stream()
	j.Profiles[0].Width, j.Profiles[0].Height = 640, 360
	f.refuse(j, code.InvalidDimensions, "not 9:16")
	j = f.stream()
	j.Options.Vertical = false
	j.Profiles[0].Width, j.Profiles[0].Height = 640, 360
	f.refuse(j, code.InvalidDimensions, "aspect ratio of the first")
	j = f.stream()
	j.Profiles[1].Width, j.Profiles[1].Height = 3840, 6826
	f.refuse(j, code.InvalidDimensions, "above 3840")
	j = f.stream()
	j.Profiles[0].FPS = 0
	f.refuse(j, code.InvalidJob, "fps is required")
	j = f.stream()
	j.Profiles[0].VideoCodec = "vp9"
	f.refuse(j, code.UnsupportedCodec, "video_codec")
	j = f.stream()
	j.Profiles[0].PixelFormat = "yuv444p"
	f.refuse(j, code.UnsupportedCodec, "pixel_format")
	j = f.stream()
	j.Profiles[0].PixelFormat = "yuv420p10le"
	f.refuse(j, code.UnsupportedCodec, "needs hevc or av1")
	j = f.stream()
	j.Profiles[0].Preset = "fast;rm -rf"
	f.refuse(j, code.UnsupportedCodec, "preset")
	j = f.stream()
	j.Profiles[0].VideoCodec = job.VideoAV1
	f.refuse(j, code.UnsupportedCodec, "preset")
	j = f.stream()
	j.Profiles[0].RateControl = "cbr"
	f.refuse(j, code.InvalidJob, "rate_control")
	j = f.stream()
	j.Profiles[0].VideoBitrate = 0
	f.refuse(j, code.InvalidJob, "video_bitrate is required")
	j = f.stream()
	j.Profiles[0].BufferSize = 0
	f.refuse(j, code.InvalidJob, "max_bitrate needs buffer_size")
	j = f.stream()
	j.Profiles[0].MaxBitrate = 100
	f.refuse(j, code.InvalidJob, "below video_bitrate")
	j = f.stream()
	j.Profiles[0].RateControl = job.RateCRF
	j.Profiles[0].CRF = 52
	f.refuse(j, code.InvalidJob, "crf must be within 0..51")
	j = f.stream()
	j.Profiles[0].SoundCodec = "opus"
	f.refuse(j, code.UnsupportedCodec, "sound_codec")
	j = f.stream()
	j.Profiles[0].SampleRate = 22050
	f.refuse(j, code.InvalidJob, "sample_rate")
	j = f.stream()
	j.Profiles[0].Channels = 6
	f.refuse(j, code.InvalidJob, "channels")

	// packaging
	j = f.stream()
	j.HLS.Format = "dash"
	f.refuse(j, code.InvalidJob, "hls.format")
	j = f.stream()
	j.HLS.SegmentDuration = 0.5
	f.refuse(j, code.InvalidJob, "hls.segment_duration")
	j = f.stream()
	j.HLS.Keyframe = 3
	f.refuse(j, code.InvalidJob, "hls.keyframe")
	j = f.stream()
	j.HLS.SegmentDuration, j.HLS.Keyframe = 3, 2
	f.refuse(j, code.InvalidJob, "whole number of hls.keyframe")
	j = f.stream()
	j.HLS.SegmentDuration, j.HLS.Keyframe = 1.5, 1.5
	j.Profiles[0].FPS = 25
	f.refuse(j, code.InvalidJob, "whole number of frames")
	j = f.stream()
	j.HLS.Format = job.HLSMPEGTS
	j.Profiles[0].VideoCodec = job.VideoHEVC
	f.refuse(j, code.UnsupportedCodec, "mpegts carries only h264")
	j = f.stream()
	j.HLS.Key = []byte("short")
	j.HLS.KeyURI = "skd://1"
	f.refuse(j, code.InvalidJob, "16 bytes")
	j = f.stream()
	j.HLS.Key = []byte("0123456789abcdef")
	f.refuse(j, code.InvalidJob, "key_uri is required")
	j = f.stream()
	j.HLS.KeyURI = "skd://1"
	f.refuse(j, code.InvalidJob, "without hls.key")
	j = f.stream()
	j.HLS.Key = []byte("0123456789abcdef")
	j.HLS.KeyURI = "skd://1\"\n#EXT-X-ENDLIST"
	f.refuse(j, code.InvalidJob, "printable ascii")

	// extras
	j = f.stream()
	j.Thumbnails[0].Format = "gif"
	f.refuse(j, code.UnsupportedCodec, "format must be")
	j = f.stream()
	j.Thumbnails[0].Quality = 101
	f.refuse(j, code.InvalidJob, "quality")
	j = f.stream()
	j.Thumbnails[0].Width = 4000
	f.refuse(j, code.InvalidDimensions, "width must be within")
	j = f.stream()
	j.Preview.Duration = -1
	f.refuse(j, code.InvalidJob, "preview.duration")
	j = f.stream()
	j.Preview.Profile.Width = 0
	f.refuse(j, code.InvalidDimensions, "preview.profile: width is required")

	// keys
	j = f.stream()
	j.PlaylistPath = "video/1111/index.mp4"
	f.refuse(j, code.InvalidPath, "playlist_path must end with .m3u8")
	j = f.stream()
	j.PlaylistPath = "video/../index.m3u8"
	f.refuse(j, code.InvalidPath, "clean relative")
	j = f.stream()
	j.PlaylistPath = "//video/index.m3u8"
	f.refuse(j, code.InvalidPath, "clean relative")
	j = f.stream()
	j.PlaylistPath = "video/a b/index.m3u8"
	f.refuse(j, code.InvalidPath, "holds a space")
	j = f.stream()
	j.Thumbnails[0].Output = "video/1111/cover.jpg"
	f.refuse(j, code.InvalidPath, "must end with .webp")
	j = f.stream()
	j.Preview.Output = j.PlaylistPath
	f.refuse(j, code.InvalidPath, "must end with .mp4")
	j = f.stream()
	j.Thumbnails = append(j.Thumbnails, j.Thumbnails[0])
	f.refuse(j, code.InvalidPath, "the same key as thumbnails[0].output")
	j = f.stream()
	j.Thumbnails[0].Output = "video/1111/720p/cover.webp"
	f.refuse(j, code.InvalidPath, "folder of rendition 720p")
	j = f.stream()
	j.Profiles = j.Profiles[:1]
	j.Preview.Output = "video/1111/init.mp4"
	f.refuse(j, code.InvalidPath, "init.mp4 of the hls rendition")
	j = f.stream()
	j.Profiles = j.Profiles[:1]
	j.Thumbnails[0].Output = "video/1111/segment_00001.webp"
	f.refuse(j, code.InvalidPath, "segment_ file name of the hls rendition")
}

// TestCheckVideo — a Video: one profile, an .mp4 output, fps may follow
// the source, the name is optional.
func TestCheckVideo(t *testing.T) {
	f := fixture{}
	f.init(t)

	j := f.video()
	j.Profile.FPS = 0
	j.Profile.Name = ""
	e := f.cfg.checkVideo(&j)
	if e != nil {
		t.Fatalf("valid video: %v", e)
	}

	j = f.video()
	j.FilePath = "video/1111/index.m3u8"
	f.refuseVideo(j, code.InvalidPath, "must end with .mp4")
	j = f.video()
	j.Profile.Height = 1300
	f.refuseVideo(j, code.InvalidDimensions, "not 9:16")
	j = f.video()
	j.ID = ""
	f.refuseVideo(j, code.InvalidJob, "id must be")
	j = f.video()
	j.Thumbnails = []job.Thumbnail{{Output: "video/1111/video.mp4"}}
	f.refuseVideo(j, code.InvalidPath, "must end with .jpg")
}

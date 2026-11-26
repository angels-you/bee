package video

import (
	"strings"
	"testing"

	"github.com/angels-you/bee/pkg/types/code"
	job "github.com/angels-you/bee/pkg/types/video"
)

// planFixture — plan inputs without ffmpeg: probes written by hand.
type planFixture struct {
	t     *testing.T
	tools *Tools
}

// init — tools whose build has no filters listed (CanTonemap false).
func (a *planFixture) init(t *testing.T) {
	a.t = t
	a.tools = &Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Threads: 1}
}

// media — the probe of a square-pixel source of w × h with audio, 10 s.
func (a *planFixture) media(w, h int) Media {
	return Media{
		Duration: 10, HasVideo: true, VideoCodec: "h264", Width: w, Height: h, SAR: "1:1",
		DisplayWidth: w, DisplayHeight: h, FPS: 30, HasAudio: true, AudioCodec: "aac", SampleRate: 48000, Channels: 2,
	}
}

// input — a Stream input of a w × h source with the given profiles, every
// default filled as bee fills it.
func (a *planFixture) input(w, h int, resize job.ResizeMode, profiles ...job.Profile) Input {
	one := 1.0
	hls := job.DefaultHLS()
	return Input{
		Source: File{Path: "source.mp4", Media: a.media(w, h)},
		Options: job.Options{
			Resize: resize, Gravity: job.GravityCenter, Background: "#102030", Speed: 1,
			SoundVolume: &one, OriginalVolume: &one,
		},
		Profiles: profiles,
		HLS:      &hls,
		LUFS:     -16,
		TruePeak: -1.5,
	}
}

// profile — a profile of w × h at 30 fps.
func (a *planFixture) profile(name string, w, h int) job.Profile {
	return job.Profile{Name: name, Width: w, Height: h, FPS: 30, VideoCodec: job.VideoH264, PixelFormat: "yuv420p",
		RateControl: job.RateVBR, VideoBitrate: 500, SoundCodec: job.SoundAAC, SoundBitrate: 96, SampleRate: 48000, Channels: 2}
}

// plan — the plan of in; fails the test on an error.
func (a *planFixture) plan(in Input) *Plan {
	a.t.Helper()
	p, e := a.tools.Plan(in)
	if e != nil {
		a.t.Fatalf("plan: %v", e)
	}
	return p
}

// refuse — the plan of in fails with code c and a message holding text.
func (a *planFixture) refuse(in Input, c code.Code, text string) {
	a.t.Helper()
	_, e := a.tools.Plan(in)
	switch {
	case e == nil:
		a.t.Errorf("planned, want %s %q", c, text)
	case e.Code != c:
		a.t.Errorf("%s %q, want %s", e.Code, e.Message, c)
	case !strings.Contains(e.Message, text):
		a.t.Errorf("message %q, want %q", e.Message, text)
	}
}

// TestPlanCover — a landscape 1920×1080 source covers vertical renditions:
// the centre column is cropped once into the canvas of the largest, every
// rendition is a scale of it.
func TestPlanCover(t *testing.T) {
	f := planFixture{}
	f.init(t)

	p := f.plan(f.input(1920, 1080, job.ResizeCover, f.profile("360p", 360, 640), f.profile("540p", 540, 960)))
	want := Geometry{Width: 540, Height: 960, CropX: 656, CropY: 0, CropWidth: 608, CropHeight: 1080, ImageWidth: 540, ImageHeight: 960}
	if p.Canvas != want {
		t.Errorf("canvas %+v, want %+v", p.Canvas, want)
	}
	if len(p.Renditions) != 2 || len(p.Skipped) != 0 {
		t.Errorf("renditions %d skipped %v", len(p.Renditions), p.Skipped)
	}
	if p.Renditions[1].Dir != "hls/540p" || p.Renditions[1].File != "hls/540p/index.m3u8" {
		t.Errorf("rendition paths %+v", p.Renditions[1])
	}

	// gravity: the crop at the left and at the right edge
	in := f.input(1920, 1080, job.ResizeCover, f.profile("540p", 540, 960))
	in.Options.Gravity = job.GravityLeft
	if f.plan(in).Canvas.CropX != 0 {
		t.Errorf("left crop at %d", f.plan(in).Canvas.CropX)
	}
	in.Options.Gravity = job.GravityBottomRight
	if f.plan(in).Canvas.CropX != 1920-608 {
		t.Errorf("right crop at %d", f.plan(in).Canvas.CropX)
	}
}

// TestPlanContain — contain pads with the background at the gravity;
// stretch takes the frame as is.
func TestPlanContain(t *testing.T) {
	f := planFixture{}
	f.init(t)

	p := f.plan(f.input(1920, 1080, job.ResizeContain, f.profile("540p", 540, 960)))
	want := Geometry{Width: 540, Height: 960, ImageWidth: 540, ImageHeight: 304, PadX: 0, PadY: 328, Pad: true}
	if p.Canvas != want {
		t.Errorf("canvas %+v, want %+v", p.Canvas, want)
	}
	graph := p.graph()
	if !strings.Contains(graph, "scale=540:304,pad=540:960:0:328:color=0x102030,setsar=1") {
		t.Errorf("contain graph %s", graph)
	}

	in := f.input(1920, 1080, job.ResizeContain, f.profile("540p", 540, 960))
	in.Options.Gravity = job.GravityTop
	if f.plan(in).Canvas.PadY != 0 {
		t.Errorf("top pad %d", f.plan(in).Canvas.PadY)
	}

	p = f.plan(f.input(1920, 1080, job.ResizeStretch, f.profile("540p", 540, 960)))
	if p.Canvas.CropWidth != 0 || p.Canvas.Pad || p.Canvas.ImageWidth != 540 || p.Canvas.ImageHeight != 960 {
		t.Errorf("stretch %+v", p.Canvas)
	}
}

// TestPlanUpscale — without Upscale a Stream skips the renditions larger
// than the source, fails when none is left; a Video fails at once; with
// Upscale everything is made.
func TestPlanUpscale(t *testing.T) {
	f := planFixture{}
	f.init(t)

	// a 720×1280 source: 1080p is skipped, the canvas is 720p
	in := f.input(720, 1280, job.ResizeCover, f.profile("360p", 360, 640), f.profile("720p", 720, 1280), f.profile("1080p", 1080, 1920))
	p := f.plan(in)
	if len(p.Renditions) != 2 || strings.Join(p.Skipped, ",") != "1080p" || p.Canvas.Width != 720 {
		t.Errorf("renditions %d skipped %v canvas %d", len(p.Renditions), p.Skipped, p.Canvas.Width)
	}

	// the order of the ladder does not matter: the canvas is the largest made
	in = f.input(720, 1280, job.ResizeCover, f.profile("1080p", 1080, 1920), f.profile("720p", 720, 1280), f.profile("360p", 360, 640))
	p = f.plan(in)
	if p.Canvas.Width != 720 {
		t.Errorf("unordered ladder: canvas %d wide", p.Canvas.Width)
	}

	// a few pixels short is rounding, not upscaling
	in = f.input(716, 1274, job.ResizeCover, f.profile("720p", 720, 1280))
	if len(f.plan(in).Renditions) != 1 {
		t.Error("720p skipped for a 716×1274 source")
	}

	// nothing fits
	in = f.input(320, 180, job.ResizeCover, f.profile("360p", 360, 640))
	f.refuse(in, code.NoRendition, "every profile")

	// a Video fails on its one profile
	in = f.input(720, 1280, job.ResizeCover, f.profile("1080p", 1080, 1920))
	in.HLS = nil
	f.refuse(in, code.NoRendition, "larger than the source")

	// with Upscale it is made
	in.Options.Upscale = true
	p = f.plan(in)
	if len(p.Renditions) != 1 || p.Renditions[0].File != "video.mp4" || p.Renditions[0].Dir != "" {
		t.Errorf("upscaled video %+v", p.Renditions)
	}
}

// TestPlanFrame — rotation metadata and Options.Rotation turn the frame;
// anamorphic pixels are squared first.
func TestPlanFrame(t *testing.T) {
	f := planFixture{}
	f.init(t)

	// a phone portrait: coded 1920×1080, shown 1080×1920
	in := f.input(1080, 1920, job.ResizeCover, f.profile("540p", 540, 960))
	in.Source.Media.Width, in.Source.Media.Height, in.Source.Media.Rotation = 1920, 1080, 90
	p := f.plan(in)
	if p.Frame != (Size{Width: 1080, Height: 1920}) || p.Canvas.CropWidth != 1080 {
		t.Errorf("portrait frame %+v canvas %+v", p.Frame, p.Canvas)
	}

	// an extra quarter turn makes it landscape
	in.Options.Rotation = 270
	p = f.plan(in)
	if p.Frame != (Size{Width: 1920, Height: 1080}) || !strings.Contains(p.graph(), "transpose=cclock") {
		t.Errorf("rotated frame %+v", p.Frame)
	}

	// anamorphic 2:1 pixels
	in = f.input(1280, 720, job.ResizeContain, f.profile("540p", 540, 960))
	in.Source.Media.Width, in.Source.Media.SAR = 640, "2:1"
	if !strings.Contains(f.plan(in).graph(), "[0:v:0]scale=1280:720,setsar=1,") {
		t.Errorf("anamorphic graph %s", f.plan(in).graph())
	}
}

// TestPlanTimeline — Start and Duration on the source, Speed on top; the
// source must reach past Start; HDR needs zscale and tonemap.
func TestPlanTimeline(t *testing.T) {
	f := planFixture{}
	f.init(t)

	in := f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	in.Options.Start, in.Options.Duration, in.Options.Speed = 2, 6, 2
	p := f.plan(in)
	if p.Start != 2 || p.Length != 6 || p.Duration != 3 {
		t.Errorf("timeline start %v length %v duration %v", p.Start, p.Length, p.Duration)
	}

	// until the end of the source
	in.Options.Duration = 0
	if f.plan(in).Length != 8 {
		t.Errorf("length to the end %v", f.plan(in).Length)
	}

	in.Options.Start = 10
	f.refuse(in, code.InvalidJob, "options.start")

	in = f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	in.Source.Media.HDR, in.Source.Media.ColorTransfer = true, "smpte2084"
	f.refuse(in, code.UnsupportedFormat, "HDR")

	f.tools.filters = map[string]bool{"zscale": true, "tonemap": true}
	if !strings.Contains(f.plan(in).graph(), "tonemap=tonemap=hable") {
		t.Error("no tone mapping for HDR")
	}

	in = f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	in.Source.Media.HasVideo = false
	f.refuse(in, code.InvalidVideo, "no video stream")
}

// TestPlanExtras — thumbnails fitted around the canvas and clamped to the
// timeline, the preview shrunk to the canvas without Upscale, the
// watermark at its corner.
func TestPlanExtras(t *testing.T) {
	f := planFixture{}
	f.init(t)

	in := f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	in.Thumbnails = []job.Thumbnail{
		{Output: "a.jpg", Format: job.ImageJPEG, Quality: 80},
		{Output: "b.png", Format: job.ImagePNG, Width: 270, Position: 99},
		{Output: "c.webp", Format: job.ImageWebP, Width: 2000, Height: 100, Position: 1},
	}
	in.Preview = &job.Preview{Start: 8, Duration: 3, Profile: f.profile("preview", 720, 1280)}
	opacity := 0.5
	in.Options.Watermark = &job.Watermark{Position: job.GravityBottomRight, Width: 100, Margin: 10, Opacity: &opacity}
	in.Watermark = &File{Path: "mark.png", Media: Media{HasVideo: true, Width: 200, Height: 50, DisplayWidth: 200, DisplayHeight: 50}}
	p := f.plan(in)

	// thumbnails
	sizes := []Size{{540, 960}, {270, 480}, {56, 100}}
	for i, s := range p.Stills {
		if s.Size != sizes[i] {
			t.Errorf("thumbnail %d %+v, want %+v", i, s.Size, sizes[i])
		}
	}
	if p.Stills[1].Position != 10 || p.Stills[1].Path != "thumbnail_1.png" {
		t.Errorf("thumbnail 1 at %v %s", p.Stills[1].Position, p.Stills[1].Path)
	}

	// the preview: 2 seconds left, the canvas size
	if p.Clip.Duration != 2 || p.Clip.Geometry.Width != 540 || p.Clip.Geometry.Height != 960 {
		t.Errorf("preview %+v", p.Clip)
	}

	// the watermark: 100×25, 10 px from the bottom right corner
	want := Mark{Width: 100, Height: 25, X: 430, Y: 925, Opacity: 0.5}
	if *p.Mark != want {
		t.Errorf("mark %+v, want %+v", *p.Mark, want)
	}
	if !strings.Contains(p.graph(), "[1:v:0]scale=100:25,format=rgba,colorchannelmixer=aa=0.5[mark];[base][mark]overlay=430:925[marked]") {
		t.Errorf("mark graph %s", p.graph())
	}

	// the preview may not start past the end
	in.Preview.Start = 10
	f.refuse(in, code.InvalidJob, "preview.start")
}

// TestGraphAudio — the audio graph for the mixes of job.go.
func TestGraphAudio(t *testing.T) {
	f := planFixture{}
	f.init(t)
	zero := 0.0
	half := 0.5

	// the original only, at its volume, slowed down to a quarter
	in := f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	in.Options.Speed = 0.25
	in.Options.OriginalVolume = &half
	graph := f.plan(in).graph()
	if !strings.Contains(graph, "[0:a:0]asetpts=PTS-STARTPTS,volume=0.5,atempo=0.5,atempo=0.5,aformat=") {
		t.Errorf("original %s", graph)
	}
	if strings.Contains(graph, "amix") || strings.Contains(graph, "dB,") {
		t.Errorf("mix or loudness without a sound %s", graph)
	}

	// mixed with the external sound, normalized (the gain Normalize
	// measured) and faded
	in = f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	in.Sound = &File{Path: "sound.mp3", Media: Media{HasAudio: true, Duration: 3}}
	in.Options.Normalize, in.Options.FadeIn, in.Options.FadeOut = true, 1, 2
	in.Preview = &job.Preview{Duration: 3, Sound: true, Profile: f.profile("preview", 540, 960)}
	p := f.plan(in)
	p.Gain = -4.25
	graph = p.graph()
	parts := []string{
		"[1:a:0]asetpts=PTS-STARTPTS,aformat=sample_rates=48000:channel_layouts=stereo[ext]",
		"[orig][ext]amix=inputs=2:duration=longest:normalize=0,apad,atrim=end=10[mix]",
		"[mix]volume=-4.25dB,afade=t=in:st=0:d=1,afade=t=out:st=8:d=2,asplit=2[b0][b1]",
		"[b1]atrim=start=0:duration=3,asetpts=PTS-STARTPTS,aformat=sample_rates=48000:channel_layouts=stereo[pa]",
	}
	for _, part := range parts {
		if !strings.Contains(graph, part) {
			t.Errorf("no %s in %s", part, graph)
		}
	}

	// the external sound only: the original muted
	in.Options.OriginalVolume = &zero
	graph = f.plan(in).graph()
	if strings.Contains(graph, "[0:a:0]") || !strings.Contains(graph, "[ext]apad") {
		t.Errorf("external only %s", graph)
	}

	// NoSound wins
	in.Options.NoSound = true
	p = f.plan(in)
	if p.HasSound || strings.Contains(p.graph(), "asplit") {
		t.Errorf("NoSound %s", p.graph())
	}

	// a silent source without a sound has no audio
	in = f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	in.Source.Media.HasAudio = false
	if f.plan(in).HasSound {
		t.Error("audio from a silent source")
	}

	// a sound without audio, a sound started past its end
	in.Sound = &File{Path: "sound.mp3", Media: Media{HasVideo: true}}
	f.refuse(in, code.InvalidSound, "no audio")
	in.Sound = &File{Path: "sound.mp3", Media: Media{HasAudio: true, Duration: 3}}
	in.Options.SoundStart = 3
	f.refuse(in, code.InvalidJob, "options.sound_start")
}

// TestGraphVideo — the visual filters take only what the job sets.
func TestGraphVideo(t *testing.T) {
	f := planFixture{}
	f.init(t)

	in := f.input(720, 1280, job.ResizeCover, f.profile("540p", 540, 960))
	graph := f.plan(in).graph()
	for _, part := range []string{"eq=", "hue=", "unsharp", "gblur", "hflip", "transpose", "zscale"} {
		if strings.Contains(graph, part) {
			t.Errorf("%s without its option: %s", part, graph)
		}
	}

	brightness := -0.25
	sharpness := 1.5
	in.Options.Brightness, in.Options.Sharpness, in.Options.Blur, in.Options.BW, in.Options.Mirror = &brightness, &sharpness, 2, true, true
	graph = f.plan(in).graph()
	part := "hflip,crop=720:1280:0:0,scale=540:960,setsar=1,eq=brightness=-0.25,hue=s=0,unsharp=luma_msize_x=5:luma_msize_y=5:luma_amount=1.5,gblur=sigma=2[base]"
	if !strings.Contains(graph, part) {
		t.Errorf("no %s in %s", part, graph)
	}
}

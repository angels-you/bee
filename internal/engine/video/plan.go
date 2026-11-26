package video

import (
	"fmt"
	"math"
	"path/filepath"

	"github.com/angels-you/bee/pkg/types/code"
	job "github.com/angels-you/bee/pkg/types/video"
)

// File — a downloaded file and its probe.
type File struct {
	Path  string
	Media Media
}

// Input — what a plan is made of: the probed files and the job, already
// validated by bee with every default of job.go filled in (Resize,
// Gravity, Background, Speed, the volumes, Watermark Position, Width and
// Opacity, the profile codecs and audio, Thumbnail Format and Quality,
// Preview Duration).
type Input struct {
	Source     File
	Sound      *File // the external sound; nil = none (or Options.NoSound)
	Watermark  *File // the watermark image of Options.Watermark; nil = none
	Options    job.Options
	Profiles   []job.Profile   // Stream: the renditions in master order; Video: the one profile
	HLS        *job.HLS        // nil for Video: a single MP4
	Thumbnails []job.Thumbnail // in job order
	Preview    *job.Preview    // nil = none

	LUFS     float64 // integrated loudness target of Options.Normalize (Config.LUFS)
	TruePeak float64 // true peak ceiling of Options.Normalize, dBTP (Config.TruePeak)
}

// Plan — what one ffmpeg run makes of the source. The source is trimmed
// (Start, Length), turned upright and by Options.Rotation (Frame), fitted
// once into the Canvas — the frame of the largest rendition — where the
// visual filters and the watermark apply, sped up, and then every output
// is scaled from the canvas: so every rendition shows the same picture on
// the same timeline. All sizes are even.
type Plan struct {
	Input

	Start    float64 // source seconds skipped
	Length   float64 // source seconds used after Start
	Speed    float64 // playback speed
	Duration float64 // the output timeline: Length / Speed

	Tonemap bool     // the source is HDR: converted to SDR first
	Frame   Size     // the source frame shown upright, square pixels, after Options.Rotation
	Canvas  Geometry // Frame fitted into the frame of the largest rendition
	Mark    *Mark    // the watermark on the canvas; nil = none

	// the linear normalization of Options.Normalize: the loudness of the mix
	// (nil = not measured) and the gain applied before the fades, dB
	Loudness *Loudness
	Gain     float64

	// audio: the outputs carry it when the original track or the external
	// sound is heard (not NoSound, volume above 0)
	HasSound bool
	original bool // the source track is mixed in
	external bool // the external sound is mixed in: ffmpeg input 1

	Renditions []Rendition // the profiles made, in Profiles order
	Skipped    []string    // the names of the Stream profiles skipped: larger than the source without Upscale
	Stills     []Still     // one per thumbnail, in job order
	Clip       *Clip       // the preview; nil = none
}

// Size of a frame, px.
type Size struct {
	Width  int
	Height int
}

// Geometry — how a frame of one size is fitted into Width × Height by the
// resize mode: cover crops CropWidth × CropHeight at CropX, CropY first;
// the picture is scaled to ImageWidth × ImageHeight; contain pads it at
// PadX, PadY with the background.
type Geometry struct {
	Width       int
	Height      int
	CropX       int
	CropY       int
	CropWidth   int // 0 = no crop
	CropHeight  int
	ImageWidth  int
	ImageHeight int
	PadX        int
	PadY        int
	Pad         bool // contain: the picture is smaller than the frame
}

// Rendition — one profile made, paths relative to the output directory:
// an HLS rendition in its own folder Dir (hls/<name>) with its media
// playlist File (hls/<name>/index.m3u8), init and segments; a Video the
// one MP4 File (video.mp4) and no Dir.
type Rendition struct {
	Profile job.Profile
	Dir     string
	File    string
}

// Still — one thumbnail: a frame of the canvas at Position of the output
// timeline (clamped to the timeline), scaled into Size.
type Still struct {
	Thumbnail job.Thumbnail
	Position  float64
	Size      Size
	Path      string
}

// Clip — the preview: Duration seconds of the output timeline from Start,
// the canvas fitted into the preview profile.
type Clip struct {
	Preview  job.Preview
	Start    float64
	Duration float64
	Geometry Geometry
	Path     string
}

// Mark — the watermark scaled to Width × Height at X, Y of the canvas.
type Mark struct {
	Width   int
	Height  int
	X       int
	Y       int
	Opacity float64
}

// Plan — the plan of one job: the timeline, the canvas and every output.
// Errors: the source has no video stream or HDR this ffmpeg cannot
// convert, Options.Start is past the end, no profile fits the source
// without Upscale, the preview starts past the end of the timeline, the
// sound has no audio or SoundStart is past its end, the watermark is not a
// picture.
func (a *Tools) Plan(in Input) (*Plan, *code.Error) {
	p := &Plan{Input: in, Speed: in.Options.Speed}
	src := in.Source.Media

	// the source
	switch {
	case !src.HasVideo:
		return nil, code.New(code.InvalidVideo, "the source has no video stream", nil)
	case src.Duration <= 0:
		return nil, code.New(code.InvalidVideo, "the source has no duration", nil)
	case src.HDR && !a.CanTonemap():
		return nil, code.New(code.UnsupportedFormat, "the source is HDR ("+src.ColorTransfer+") and this ffmpeg cannot convert it to SDR", nil)
	case in.Options.Start >= src.Duration:
		return nil, code.New(code.InvalidJob, fmt.Sprintf("options.start %.3fs is past the end of the source (%.3fs)", in.Options.Start, src.Duration), nil)
	}
	p.Tonemap = src.HDR

	// the other files
	switch {
	case in.Sound != nil && !in.Sound.Media.HasAudio:
		return nil, code.New(code.InvalidSound, "the sound has no audio stream", nil)
	case in.Sound != nil && in.Options.SoundStart >= in.Sound.Media.Duration:
		return nil, code.New(code.InvalidJob, fmt.Sprintf("options.sound_start %.3fs is past the end of the sound (%.3fs)", in.Options.SoundStart, in.Sound.Media.Duration), nil)
	case in.Watermark != nil && !in.Watermark.Media.HasVideo:
		return nil, code.New(code.InvalidWatermark, "the watermark is not a picture", nil)
	case in.Watermark != nil && (in.Watermark.Media.DisplayWidth <= 0 || in.Watermark.Media.DisplayHeight <= 0):
		return nil, code.New(code.InvalidWatermark, "the watermark has no size", nil)
	}

	// the timeline: Start and Duration on the source, then Speed
	p.Start = in.Options.Start
	p.Length = src.Duration - p.Start
	if in.Options.Duration > 0 {
		p.Length = min(p.Length, in.Options.Duration)
	}
	p.Duration = p.Length / p.Speed

	// the frame: upright, then the extra rotation
	p.Frame = Size{Width: src.DisplayWidth, Height: src.DisplayHeight}
	if in.Options.Rotation == 90 || in.Options.Rotation == 270 {
		p.Frame = Size{Width: src.DisplayHeight, Height: src.DisplayWidth}
	}

	// audio: what is left after NoSound and the volumes
	p.original = !in.Options.NoSound && src.HasAudio && *in.Options.OriginalVolume > 0
	p.external = !in.Options.NoSound && in.Sound != nil && *in.Options.SoundVolume > 0
	p.HasSound = p.original || p.external

	// the outputs
	e := p.renditions()
	if e != nil {
		return nil, e
	}
	p.stills()
	e = p.clip()
	if e != nil {
		return nil, e
	}
	p.mark()
	return p, nil
}

// renditions — the profiles that fit the frame (every one with Upscale),
// the canvas of the largest of them. A Stream skips a profile larger than
// the source, a Video fails on it.
func (a *Plan) renditions() *code.Error {
	var top job.Profile
	for _, prof := range a.Profiles {
		_, scale := a.fit(a.Frame, prof.Width, prof.Height)

		// without Upscale the source is never enlarged (1 % of rounding allowed)
		if scale > 1.01 && !a.Options.Upscale {
			if a.HLS == nil {
				return code.New(code.NoRendition, fmt.Sprintf("profile %dx%d is larger than the source %dx%d and upscale is off", prof.Width, prof.Height, a.Frame.Width, a.Frame.Height), nil)
			}
			a.Skipped = append(a.Skipped, prof.Name)
			continue
		}

		// where the files of the profile go
		r := Rendition{Profile: prof, File: "video.mp4"}
		if a.HLS != nil {
			r.Dir = filepath.Join("hls", prof.Name)
			r.File = filepath.Join(r.Dir, "index.m3u8")
		}
		a.Renditions = append(a.Renditions, r)

		// the canvas is the largest rendition
		if prof.Width*prof.Height > top.Width*top.Height {
			top = prof
		}
	}
	if len(a.Renditions) == 0 {
		return code.New(code.NoRendition, fmt.Sprintf("every profile is larger than the source %dx%d and upscale is off", a.Frame.Width, a.Frame.Height), nil)
	}

	a.Canvas, _ = a.fit(a.Frame, top.Width, top.Height)
	return nil
}

// stills — every thumbnail: its box (0 = the canvas side) fitted around
// the canvas, the aspect kept, never above the canvas without Upscale; its
// position clamped to the timeline (the last frame is held).
func (a *Plan) stills() {
	for i, t := range a.Thumbnails {
		cw := float64(a.Canvas.Width)
		ch := float64(a.Canvas.Height)

		// the scale of the box: the tighter side, an unset side is free
		scale := math.Inf(1)
		if t.Width > 0 {
			scale = float64(t.Width) / cw
		}
		if t.Height > 0 {
			scale = min(scale, float64(t.Height)/ch)
		}
		if math.IsInf(scale, 1) {
			scale = 1
		}
		if !a.Options.Upscale {
			scale = min(scale, 1)
		}

		a.Stills = append(a.Stills, Still{
			Thumbnail: t,
			Position:  min(t.Position, a.Duration),
			Size:      Size{Width: max(2, a.even(cw*scale)), Height: max(2, a.even(ch*scale))},
			Path:      fmt.Sprintf("thumbnail_%d.%s", i, t.Format),
		})
	}
}

// clip — the preview: from its start on the timeline, at most its
// duration; the canvas fitted into its profile, the profile shrunk to the
// canvas without Upscale.
func (a *Plan) clip() *code.Error {
	if a.Preview == nil {
		return nil
	}
	if a.Preview.Start >= a.Duration {
		return code.New(code.InvalidJob, fmt.Sprintf("preview.start %.3fs is past the end of the timeline (%.3fs)", a.Preview.Start, a.Duration), nil)
	}

	// the frame of the preview
	canvas := Size{Width: a.Canvas.Width, Height: a.Canvas.Height}
	width := a.Preview.Profile.Width
	height := a.Preview.Profile.Height
	g, scale := a.fit(canvas, width, height)
	if scale > 1.01 && !a.Options.Upscale {
		g, _ = a.fit(canvas, max(2, a.even(float64(width)/scale)), max(2, a.even(float64(height)/scale)))
	}

	a.Clip = &Clip{
		Preview:  *a.Preview,
		Start:    a.Preview.Start,
		Duration: min(a.Preview.Duration, a.Duration-a.Preview.Start),
		Geometry: g,
		Path:     "preview.mp4",
	}
	return nil
}

// mark — the watermark on the canvas: its width (never wider than the
// canvas), the height of its aspect, placed by Position and Margin.
func (a *Plan) mark() {
	if a.Watermark == nil {
		return
	}
	w := a.Options.Watermark
	img := a.Watermark.Media

	width := min(w.Width, a.Canvas.Width)
	height := max(1, int(math.Round(float64(width)*float64(img.DisplayHeight)/float64(img.DisplayWidth))))

	// the margin on the selected edges, the rest by gravity
	gx, gy := a.gravity(w.Position)
	x := float64(w.Margin) + float64(a.Canvas.Width-width-2*w.Margin)*gx
	y := float64(w.Margin) + float64(a.Canvas.Height-height-2*w.Margin)*gy

	a.Mark = &Mark{
		Width:   width,
		Height:  height,
		X:       max(0, int(math.Round(x))),
		Y:       max(0, int(math.Round(y))),
		Opacity: *w.Opacity,
	}
}

// fit — src fitted into width × height by Options.Resize and Gravity, and
// the scale of the picture (above 1 = enlarged):
//
//	cover    the picture covers the frame, the overflow is cropped (the
//	         crop is taken from src first, then scaled)
//	contain  the picture fits inside the frame, the rest is padded
//	stretch  the picture is the frame, the aspect is not kept
func (a *Plan) fit(src Size, width, height int) (Geometry, float64) {
	g := Geometry{Width: width, Height: height, ImageWidth: width, ImageHeight: height}
	sw := float64(src.Width)
	sh := float64(src.Height)
	sx := float64(width) / sw
	sy := float64(height) / sh
	gx, gy := a.gravity(a.Options.Gravity)

	switch a.Options.Resize {
	case job.ResizeCover:
		scale := max(sx, sy)
		g.CropWidth = min(max(2, a.even(float64(width)/scale)), src.Width)
		g.CropHeight = min(max(2, a.even(float64(height)/scale)), src.Height)
		g.CropX = a.even(float64(src.Width-g.CropWidth) * gx)
		g.CropY = a.even(float64(src.Height-g.CropHeight) * gy)
		return g, scale

	case job.ResizeContain:
		scale := min(sx, sy)
		g.ImageWidth = min(max(2, a.even(sw*scale)), width)
		g.ImageHeight = min(max(2, a.even(sh*scale)), height)
		g.PadX = a.even(float64(width-g.ImageWidth) * gx)
		g.PadY = a.even(float64(height-g.ImageHeight) * gy)
		g.Pad = g.ImageWidth != width || g.ImageHeight != height
		return g, scale
	}

	// stretch: enlarged when either side grows
	return g, max(sx, sy)
}

// markInput — the ffmpeg input of the watermark: after the source and the
// external sound, when there is one.
func (a *Plan) markInput() int {
	if a.external {
		return 2
	}
	return 1
}

// gravity — where the free space goes: the share of it before the picture
// on each axis (0 = left/top, 0.5 = centre, 1 = right/bottom).
func (a *Plan) gravity(g job.Gravity) (float64, float64) {
	switch g {
	case job.GravityTop:
		return 0.5, 0
	case job.GravityBottom:
		return 0.5, 1
	case job.GravityLeft:
		return 0, 0.5
	case job.GravityRight:
		return 1, 0.5
	case job.GravityTopLeft:
		return 0, 0
	case job.GravityTopRight:
		return 1, 0
	case job.GravityBottomLeft:
		return 0, 1
	case job.GravityBottomRight:
		return 1, 1
	}
	return 0.5, 0.5
}

// even — x rounded to the nearest even number: yuv420p needs even sides
// and offsets.
func (a *Plan) even(x float64) int {
	return int(math.Round(x/2)) * 2
}

package video

import (
	"fmt"
	"strconv"
	"strings"

	job "github.com/angels-you/bee/pkg/types/video"
)

// graph — the filter_complex of the run. The source is decoded once and
// goes through one shared chain (HDR→SDR, square pixels, rotation and
// mirror, the canvas, the visual filters, the watermark, the speed), then
// split: one branch per rendition, one for the preview, one per
// thumbnail. The audio is mixed, timed and normalized once and split the
// same way. Every value in the graph is a number or a colour bee
// validated: nothing of the job reaches it as free text.
//
// Labels: [v<i>] [a<i>] rendition i, [pv] [pa] the preview, [t<i>]
// thumbnail i.
func (a *Plan) graph() string {
	var chains []string

	// the shared video chain up to [base]
	chains = append(chains, "[0:v:0]"+strings.Join(a.videoChain(), ",")+"[base]")

	// the watermark over the canvas
	last := "[base]"
	if a.Mark != nil {
		chains = append(chains, a.markChain())
		chains = append(chains, fmt.Sprintf("[base][mark]overlay=%d:%d[marked]", a.Mark.X, a.Mark.Y))
		last = "[marked]"
	}

	// the speed, then one copy of the canvas per output
	outputs := len(a.Renditions) + len(a.Stills)
	if a.Clip != nil {
		outputs++
	}

	split := last + "setpts=(PTS-STARTPTS)/" + a.num(a.Speed) + ",split=" + strconv.Itoa(outputs)
	for i := 0; i < outputs; i++ {
		split += fmt.Sprintf("[c%d]", i)
	}
	chains = append(chains, split)

	// one branch per output, in the order of the split: renditions
	branch := 0
	for i, r := range a.Renditions {
		var f []string

		// the frame rate of the profile
		rate := a.rate(r.Profile.FPS)
		if rate != "" {
			f = append(f, "fps="+rate)
		}

		// the frame of the profile
		f = append(f, fmt.Sprintf("scale=%d:%d", r.Profile.Width, r.Profile.Height), "setsar=1", "format="+r.Profile.PixelFormat)
		chains = append(chains, fmt.Sprintf("[c%d]%s[v%d]", branch, strings.Join(f, ","), i))
		branch++
	}

	// the preview: its part of the timeline, fitted into its profile
	if a.Clip != nil {
		f := []string{"trim=start=" + a.num(a.Clip.Start) + ":duration=" + a.num(a.Clip.Duration), "setpts=PTS-STARTPTS"}

		// the frame rate of the preview profile
		rate := a.rate(a.Clip.Preview.Profile.FPS)
		if rate != "" {
			f = append(f, "fps="+rate)
		}

		// the frame of the preview
		f = append(f, a.geometry(a.Clip.Geometry), "format="+a.Clip.Preview.Profile.PixelFormat)
		chains = append(chains, fmt.Sprintf("[c%d]%s[pv]", branch, strings.Join(f, ",")))
		branch++
	}

	// thumbnails: the last frame is held for a second, so a position at
	// the very end still has a frame
	for i, s := range a.Stills {
		f := []string{
			"tpad=stop_mode=clone:stop_duration=1",
			"trim=start=" + a.num(s.Position),
			"setpts=PTS-STARTPTS",
			fmt.Sprintf("scale=%d:%d", s.Size.Width, s.Size.Height),
			"setsar=1",
			"format=" + a.stillFormat(s.Thumbnail.Format),
		}
		chains = append(chains, fmt.Sprintf("[c%d]%s[t%d]", branch, strings.Join(f, ","), i))
		branch++
	}

	// the audio
	if a.HasSound {
		chains = append(chains, a.audioChains()...)
	}
	return strings.Join(chains, ";")
}

// videoChain — the filters of the shared chain from the decoded source
// (ffmpeg has already turned it upright by its display matrix) to the
// canvas with the visual filters.
func (a *Plan) videoChain() []string {
	var f []string
	src := a.Source.Media
	o := a.Options

	// HDR (PQ, HLG) to SDR bt709: linear light, hable tone mapping
	if a.Tonemap {
		f = append(f, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
			"tonemap=tonemap=hable:desat=0", "zscale=t=bt709:m=bt709:r=tv", "format=yuv420p")
	}

	// square pixels
	if src.SAR != "1:1" {
		f = append(f, fmt.Sprintf("scale=%d:%d", src.DisplayWidth, src.DisplayHeight), "setsar=1")
	}

	// the extra clockwise rotation
	switch o.Rotation {
	case 90:
		f = append(f, "transpose=clock")
	case 180:
		f = append(f, "hflip", "vflip")
	case 270:
		f = append(f, "transpose=cclock")
	}

	// the mirror
	if o.Mirror {
		f = append(f, "hflip")
	}

	// the canvas
	f = append(f, a.geometry(a.Canvas))

	// colour: eq takes only the values the job sets
	var eq []string
	if o.Brightness != nil {
		eq = append(eq, "brightness="+a.num(*o.Brightness))
	}
	if o.Contrast != nil {
		eq = append(eq, "contrast="+a.num(*o.Contrast))
	}
	if o.Saturation != nil {
		eq = append(eq, "saturation="+a.num(*o.Saturation))
	}
	if len(eq) > 0 {
		f = append(f, "eq="+strings.Join(eq, ":"))
	}

	// black and white
	if o.BW {
		f = append(f, "hue=s=0")
	}

	// detail: a 5×5 luma unsharp mask
	if o.Sharpness != nil && *o.Sharpness > 0 {
		f = append(f, "unsharp=luma_msize_x=5:luma_msize_y=5:luma_amount="+a.num(*o.Sharpness))
	}

	// a gaussian blur
	if o.Blur > 0 {
		f = append(f, "gblur=sigma="+a.num(o.Blur))
	}
	return f
}

// markChain — the watermark picture scaled to its place and made
// translucent by Opacity, as [mark].
func (a *Plan) markChain() string {
	// the size of the watermark, with alpha
	f := []string{fmt.Sprintf("scale=%d:%d", a.Mark.Width, a.Mark.Height), "format=rgba"}

	// the opacity
	if a.Mark.Opacity < 1 {
		f = append(f, "colorchannelmixer=aa="+a.num(a.Mark.Opacity))
	}

	return fmt.Sprintf("[%d:v:0]%s[mark]", a.markInput(), strings.Join(f, ","))
}

// geometry — the filters that fit a frame into g, ending in square
// pixels.
func (a *Plan) geometry(g Geometry) string {
	var f []string

	// cover: the crop first
	if g.CropWidth > 0 {
		f = append(f, fmt.Sprintf("crop=%d:%d:%d:%d", g.CropWidth, g.CropHeight, g.CropX, g.CropY))
	}

	// the picture
	f = append(f, fmt.Sprintf("scale=%d:%d", g.ImageWidth, g.ImageHeight))

	// contain: the background around it
	if g.Pad {
		f = append(f, fmt.Sprintf("pad=%d:%d:%d:%d:color=0x%s", g.Width, g.Height, g.PadX, g.PadY, strings.TrimPrefix(a.Options.Background, "#")))
	}

	// square pixels
	f = append(f, "setsar=1")
	return strings.Join(f, ",")
}

// mixChains — the audio of the timeline up to the point where the
// normalization gain applies, ending in [mix]: the original track (its
// volume, the speed) and the external sound (its volume; looped by its
// input) mixed at their own levels, padded with silence and cut to exactly
// the timeline. The encode and the loudness pass both use it, so the pass
// measures exactly what is encoded.
func (a *Plan) mixChains() []string {
	var chains []string
	var mix []string
	o := a.Options
	common := "aformat=sample_rates=48000:channel_layouts=stereo"

	// the original track: trimmed with the source, at the speed
	if a.original {
		f := []string{"asetpts=PTS-STARTPTS"}
		if *o.OriginalVolume != 1 {
			f = append(f, "volume="+a.num(*o.OriginalVolume))
		}
		f = append(f, a.tempo()...)
		f = append(f, common)
		chains = append(chains, "[0:a:0]"+strings.Join(f, ",")+"[orig]")
		mix = append(mix, "[orig]")
	}

	// the external sound from SoundStart, never sped up
	if a.external {
		f := []string{"asetpts=PTS-STARTPTS"}
		if *o.SoundVolume != 1 {
			f = append(f, "volume="+a.num(*o.SoundVolume))
		}
		f = append(f, common)
		chains = append(chains, "[1:a:0]"+strings.Join(f, ",")+"[ext]")
		mix = append(mix, "[ext]")
	}

	// both mixed at their own volumes (amix halves them by default)
	var f []string
	if len(mix) == 2 {
		f = append(f, "amix=inputs=2:duration=longest:normalize=0")
	}

	// exactly the timeline: silence after a short track, the rest cut
	f = append(f, "apad", "atrim=end="+a.num(a.Duration))
	chains = append(chains, strings.Join(mix, "")+strings.Join(f, ",")+"[mix]")
	return chains
}

// audioChains — the final audio as [a<i>] per rendition and [pa] for a
// preview with sound: the mix (mixChains), the linear normalization gain
// (Gain, measured by Normalize), the fades, split per output.
func (a *Plan) audioChains() []string {
	chains := a.mixChains()
	o := a.Options
	preview := a.Clip != nil && a.Clip.Preview.Sound

	// one linear gain
	var f []string
	if o.Normalize {
		f = append(f, "volume="+a.num(a.Gain)+"dB")
	}

	// then the fades of the final mix
	if o.FadeIn > 0 {
		f = append(f, "afade=t=in:st=0:d="+a.num(min(o.FadeIn, a.Duration)))
	}
	if o.FadeOut > 0 {
		out := min(o.FadeOut, a.Duration)
		f = append(f, "afade=t=out:st="+a.num(a.Duration-out)+":d="+a.num(out))
	}

	// one copy per output with sound
	outputs := len(a.Renditions)
	if preview {
		outputs++
	}

	split := "asplit=" + strconv.Itoa(outputs)
	for i := 0; i < outputs; i++ {
		split += fmt.Sprintf("[b%d]", i)
	}
	f = append(f, split)
	chains = append(chains, "[mix]"+strings.Join(f, ","))

	// each output in the sample rate and channels of its profile
	for i, r := range a.Renditions {
		chains = append(chains, fmt.Sprintf("[b%d]%s[a%d]", i, a.audioFormat(r.Profile), i))
	}

	// the preview: its part of the timeline
	if preview {
		trim := "atrim=start=" + a.num(a.Clip.Start) + ":duration=" + a.num(a.Clip.Duration)
		chains = append(chains, fmt.Sprintf("[b%d]%s,asetpts=PTS-STARTPTS,%s[pa]", len(a.Renditions), trim, a.audioFormat(a.Clip.Preview.Profile)))
	}
	return chains
}

// tempo — the atempo filters of Speed: one atempo takes 0.5..2, a speed
// outside is a chain of them; none at speed 1.
func (a *Plan) tempo() []string {
	var f []string
	speed := a.Speed

	// a fast speed: halves of it
	for speed > 2 {
		f = append(f, "atempo=2")
		speed /= 2
	}

	// a slow speed: doubles of it
	for speed < 0.5 {
		f = append(f, "atempo=0.5")
		speed /= 0.5
	}

	// the rest
	if speed != 1 {
		f = append(f, "atempo="+a.num(speed))
	}
	return f
}

// rate — the constant frame rate of an output: the profile's; for FPS 0
// the source rate, held through a speed change (frames dropped or
// repeated) and left alone at speed 1, so a variable frame rate source
// keeps its timing. Empty = no fps filter.
func (a *Plan) rate(fps int) string {
	switch {
	case fps > 0:
		return strconv.Itoa(fps)
	case a.Speed != 1 && a.Source.Media.FPS > 0:
		return a.num(a.Source.Media.FPS)
	}
	return ""
}

// audioFormat — the aformat of a profile: its sample rate, mono or stereo.
func (a *Plan) audioFormat(p job.Profile) string {
	layout := "stereo"
	if p.Channels == 1 {
		layout = "mono"
	}

	return "aformat=sample_rates=" + strconv.Itoa(p.SampleRate) + ":channel_layouts=" + layout
}

// stillFormat — the pixel format a thumbnail is encoded in: full range
// yuv for jpeg, rgb for png, yuv420p for webp and avif.
func (a *Plan) stillFormat(f job.ImageFormat) string {
	switch f {
	case job.ImageJPEG:
		return "yuvj420p"
	case job.ImagePNG:
		return "rgb24"
	}
	return "yuv420p"
}

// num — x as a filter argument: decimal, no exponent, shortest form.
func (a *Plan) num(x float64) string {
	return strconv.FormatFloat(x, 'f', -1, 64)
}

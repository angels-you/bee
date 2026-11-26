package engine

import (
	"encoding/hex"
	"fmt"
	"github.com/angels-you/bee/pkg/tools"
	"math"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/angels-you/bee/internal/config"
	"github.com/angels-you/bee/pkg/types/code"
	job "github.com/angels-you/bee/pkg/types/video"
)

// videoCheck checks a video job against the job contract and the bee config,
// filling in every default: job.go and BEE_VIDEO_*
type videoCheck struct {
	cfg *config.Config
}

// checkStream — a Stream job against the job contract and the config,
// normalized in place: every default of job.go and every worker default
// of the config filled in, the output keys cleaned (one leading "/"
// trimmed). The slices and pointers of the job are copied first, so the
// caller's job stays as it was. Whether the ffmpeg build encodes the
// codecs is not checked here (video.Tools.CanEncode).
func (a *videoCheck) checkStream(j *job.Stream) *code.Error {
	j.Profiles = slices.Clone(j.Profiles)
	j.Thumbnails = slices.Clone(j.Thumbnails)
	if j.Preview != nil {
		preview := *j.Preview
		j.Preview = &preview
	}

	// the job and its downloads
	if !tools.ID(j.ID) {
		return code.New(code.InvalidJob, "id must be 1..128 characters of A-Z, a-z, 0-9, _ and -", nil)
	}
	e := a.checkURL("input", j.Input)
	if e != nil {
		return e
	}
	e = a.checkOptions(&j.Options)
	if e != nil {
		return e
	}

	// the renditions: unique folder names, a constant frame rate, one aspect
	if len(j.Profiles) == 0 {
		return code.New(code.InvalidJob, "profiles: at least one is required", nil)
	}
	names := map[string]bool{}
	for i := range j.Profiles {
		p := &j.Profiles[i]
		field := fmt.Sprintf("profiles[%d]", i)

		e = a.checkName(field, p.Name)
		if e != nil {
			return e
		}
		e = a.checkProfile(field, p, j.Options.Vertical)
		if e != nil {
			return e
		}

		switch {
		case names[p.Name]:
			return code.New(code.InvalidJob, field+": name "+p.Name+" is used twice", nil)
		case p.FPS == 0:
			return code.New(code.InvalidJob, field+": fps is required for a stream rendition", nil)
		case !a.sameAspect(j.Profiles[0], *p):
			return code.New(code.InvalidDimensions, field+": every rendition must have the aspect ratio of the first", nil)
		}
		names[p.Name] = true
	}

	// the packaging
	e = a.checkHLS(&j.HLS, j.Profiles)
	if e != nil {
		return e
	}

	// the extras
	for i := range j.Thumbnails {
		e = a.checkThumbnail(fmt.Sprintf("thumbnails[%d]", i), &j.Thumbnails[i])
		if e != nil {
			return e
		}
	}
	if j.Preview != nil {
		e = a.checkPreview(j.Preview, j.Options.Vertical)
		if e != nil {
			return e
		}
	}

	// the output keys
	keys := map[string]string{}
	j.Output, e = a.key("output", j.Output, ".m3u8", keys)
	if e != nil {
		return e
	}
	e = a.extraKeys(j.Thumbnails, j.Preview, keys)
	if e != nil {
		return e
	}
	return a.renditionKeys(j.Output, j.Profiles, keys)
}

// checkVideo — a Video job against the job contract and the config,
// normalized in place like checkStream.
func (a *videoCheck) checkVideo(j *job.Video) *code.Error {
	j.Thumbnails = slices.Clone(j.Thumbnails)
	if j.Preview != nil {
		preview := *j.Preview
		j.Preview = &preview
	}

	// the job and its downloads
	if !tools.ID(j.ID) {
		return code.New(code.InvalidJob, "id must be 1..128 characters of A-Z, a-z, 0-9, _ and -", nil)
	}
	e := a.checkURL("input", j.Input)
	if e != nil {
		return e
	}
	e = a.checkOptions(&j.Options)
	if e != nil {
		return e
	}

	// the one profile; its name is only echoed in the result
	if j.Profile.Name != "" {
		e = a.checkName("profile", j.Profile.Name)
		if e != nil {
			return e
		}
	}
	e = a.checkProfile("profile", &j.Profile, j.Options.Vertical)
	if e != nil {
		return e
	}

	// the extras
	for i := range j.Thumbnails {
		e = a.checkThumbnail(fmt.Sprintf("thumbnails[%d]", i), &j.Thumbnails[i])
		if e != nil {
			return e
		}
	}
	if j.Preview != nil {
		e = a.checkPreview(j.Preview, j.Options.Vertical)
		if e != nil {
			return e
		}
	}

	// the output keys
	keys := map[string]string{}
	j.Output, e = a.key("output", j.Output, ".mp4", keys)
	if e != nil {
		return e
	}
	return a.extraKeys(j.Thumbnails, j.Preview, keys)
}

// checkURL — a download url: valid, https (http only with
// AllowInsecureSource), its host allowed. The message never quotes the
// url: its query is a presigned signature.
func (a *videoCheck) checkURL(field, raw string) *code.Error {
	u, err := url.Parse(raw)
	switch {
	case raw == "":
		return code.New(code.InvalidJob, field+" is required", nil)
	case err != nil:
		return code.New(code.InvalidJob, field+" is not a valid url", nil)
	case u.Host == "":
		return code.New(code.InvalidJob, field+" has no host", nil)
	case u.Scheme == "http" && !a.cfg.AllowInsecure:
		return code.New(code.InvalidJob, field+" must be https", nil)
	case u.Scheme != "https" && u.Scheme != "http":
		return code.New(code.InvalidJob, field+" must be https", nil)
	case !a.cfg.SourceHostAllowed(u.Hostname()):
		return code.New(code.InvalidJob, field+" host is not allowed", nil)
	}
	return nil
}

// checkOptions — the options with their defaults: contain (cover when
// Vertical), centre, black, speed 1, sound volume 1, original volume 0
// with an external sound and 1 without; NoSound drops the external sound
// (it is not even downloaded). The watermark is copied and gets its
// defaults: bottom-right, the bee default, opaque.
func (a *videoCheck) checkOptions(o *job.Options) *code.Error {
	// defaults of job.go
	if o.Resize == "" {
		o.Resize = job.ResizeContain
		if o.Vertical {
			o.Resize = job.ResizeCover
		}
	}
	if o.Gravity == "" {
		o.Gravity = job.GravityCenter
	}
	if o.Background == "" {
		o.Background = "#000000"
	}
	if o.Speed == 0 {
		o.Speed = 1
	}
	if o.LUFS == 0 {
		o.LUFS = a.cfg.Video.LUFS
	}
	if o.TruePeak == 0 {
		o.TruePeak = a.cfg.Video.TruePeak
	}
	if o.NoSound {
		o.Sound = ""
	}
	if o.SoundVolume == nil {
		one := 1.0
		o.SoundVolume = &one
	}
	if o.OriginalVolume == nil {
		volume := 1.0
		if o.Sound != "" {
			volume = 0
		}
		o.OriginalVolume = &volume
	}

	// the background as #rrggbb
	background, ok := a.color(o.Background)
	if !ok {
		return code.New(code.InvalidJob, "options.background must be #rgb or #rrggbb", nil)
	}
	o.Background = background

	// geometry and timeline
	switch {
	case !a.validResize(o.Resize):
		return code.New(code.InvalidJob, "options.resize must be cover, contain or stretch", nil)
	case !a.validGravity(o.Gravity):
		return code.New(code.InvalidJob, "options.gravity is not a known gravity", nil)
	case o.Rotation != 0 && o.Rotation != 90 && o.Rotation != 180 && o.Rotation != 270:
		return code.New(code.InvalidJob, "options.rotation must be 0, 90, 180 or 270", nil)
	case !a.seconds(o.Start):
		return code.New(code.InvalidJob, "options.start must be a non-negative number of seconds", nil)
	case !a.seconds(o.Duration):
		return code.New(code.InvalidJob, "options.duration must be a non-negative number of seconds", nil)
	case !(o.Speed >= 0.25 && o.Speed <= 4):
		return code.New(code.InvalidJob, "options.speed must be within 0.25..4", nil)
	}

	// visual filters
	switch {
	case o.Brightness != nil && !(*o.Brightness >= -1 && *o.Brightness <= 1):
		return code.New(code.InvalidJob, "options.brightness must be within -1..1", nil)
	case o.Contrast != nil && !(*o.Contrast >= 0 && *o.Contrast <= 2):
		return code.New(code.InvalidJob, "options.contrast must be within 0..2", nil)
	case o.Saturation != nil && !(*o.Saturation >= 0 && *o.Saturation <= 3):
		return code.New(code.InvalidJob, "options.saturation must be within 0..3", nil)
	case o.Sharpness != nil && !(*o.Sharpness >= 0 && *o.Sharpness <= 2):
		return code.New(code.InvalidJob, "options.sharpness must be within 0..2", nil)
	case !(o.Blur >= 0 && o.Blur <= 50):
		return code.New(code.InvalidJob, "options.blur must be within 0..50", nil)
	}

	// audio
	switch {
	case !a.seconds(o.SoundStart):
		return code.New(code.InvalidJob, "options.sound_start must be a non-negative number of seconds", nil)
	case !(*o.SoundVolume >= 0 && *o.SoundVolume <= 4):
		return code.New(code.InvalidJob, "options.sound_volume must be within 0..4", nil)
	case !(*o.OriginalVolume >= 0 && *o.OriginalVolume <= 4):
		return code.New(code.InvalidJob, "options.original_volume must be within 0..4", nil)
	case !(o.FadeIn >= 0 && o.FadeIn <= a.cfg.Video.MaxDuration.Seconds()):
		return code.New(code.InvalidJob, "options.fade_in must be within 0..BEE_VIDEO_MAX_DURATION", nil)
	case !(o.FadeOut >= 0 && o.FadeOut <= a.cfg.Video.MaxDuration.Seconds()):
		return code.New(code.InvalidJob, "options.fade_out must be within 0..BEE_VIDEO_MAX_DURATION", nil)
	case !(o.LUFS >= -70 && o.LUFS <= -5):
		return code.New(code.InvalidJob, "options.lufs must be within -70..-5", nil)
	case !(o.TruePeak >= -9 && o.TruePeak <= 0):
		return code.New(code.InvalidJob, "options.true_peak must be within -9..0", nil)
	}
	if o.Sound != "" {
		e := a.checkURL("options.sound", o.Sound)
		if e != nil {
			return e
		}
	}

	// the watermark
	if o.Watermark == nil {
		return nil
	}
	mark := *o.Watermark
	o.Watermark = &mark
	return a.checkWatermark(o.Watermark)
}

// checkWatermark — the watermark with its defaults: bottom-right,
// the bee default wide, opaque.
func (a *videoCheck) checkWatermark(w *job.Watermark) *code.Error {
	if w.Position == "" {
		w.Position = job.GravityBottomRight
	}
	if w.Width == 0 {
		w.Width = a.cfg.Video.WatermarkWidth
	}
	if w.Opacity == nil {
		one := 1.0
		w.Opacity = &one
	}

	switch {
	case !a.validGravity(w.Position):
		return code.New(code.InvalidJob, "options.watermark.position is not a known gravity", nil)
	case w.Width < 1 || w.Width > a.cfg.Video.MaxDimension:
		return code.New(code.InvalidDimensions, fmt.Sprintf("options.watermark.width must be within 1..%d", a.cfg.Video.MaxDimension), nil)
	case w.Margin < 0 || w.Margin > a.cfg.Video.MaxDimension:
		return code.New(code.InvalidDimensions, fmt.Sprintf("options.watermark.margin must be within 0..%d", a.cfg.Video.MaxDimension), nil)
	case !(*w.Opacity >= 0 && *w.Opacity <= 1):
		return code.New(code.InvalidJob, "options.watermark.opacity must be within 0..1", nil)
	}
	return a.checkURL("options.watermark.input", w.Input)
}

// checkProfile — one output profile with its defaults: h264, yuv420p,
// vbr, aac at the bee default, 48 kHz, stereo. field names it in the
// errors. Vertical asks for 9:16 (a side may be off by the rounding to an
// even pixel).
func (a *videoCheck) checkProfile(field string, p *job.Profile, vertical bool) *code.Error {
	// defaults of job.go and of the worker
	if p.VideoCodec == "" {
		p.VideoCodec = job.VideoH264
	}
	if p.PixelFormat == "" {
		p.PixelFormat = "yuv420p"
	}
	if p.RateControl == "" {
		p.RateControl = job.RateVBR
	}
	if p.SoundCodec == "" {
		p.SoundCodec = job.SoundAAC
	}
	if p.SoundBitrate == 0 {
		p.SoundBitrate = a.cfg.Video.SoundBitrate
	}
	if p.SampleRate == 0 {
		p.SampleRate = 48000
	}
	if p.Channels == 0 {
		p.Channels = 2
	}

	// the frame; 9:16 allows a side off by the rounding to an even pixel
	aspect := p.Height*9 - p.Width*16
	switch {
	case p.Width <= 0:
		return code.New(code.InvalidDimensions, field+": width is required", nil)
	case p.Height <= 0:
		return code.New(code.InvalidDimensions, field+": height is required", nil)
	case p.Width%2 != 0:
		return code.New(code.InvalidDimensions, field+": width must be even", nil)
	case p.Height%2 != 0:
		return code.New(code.InvalidDimensions, field+": height must be even", nil)
	case p.Width > a.cfg.Video.MaxDimension:
		return code.New(code.InvalidDimensions, fmt.Sprintf("%s: width is above %d", field, a.cfg.Video.MaxDimension), nil)
	case p.Height > a.cfg.Video.MaxDimension:
		return code.New(code.InvalidDimensions, fmt.Sprintf("%s: height is above %d", field, a.cfg.Video.MaxDimension), nil)
	case vertical && (aspect > 18 || aspect < -18):
		return code.New(code.InvalidDimensions, fmt.Sprintf("%s: %dx%d is not 9:16 and options.vertical is set", field, p.Width, p.Height), nil)
	case p.FPS < 0 || p.FPS > 120:
		return code.New(code.InvalidJob, field+": fps must be within 0..120", nil)
	}

	// the video encoder
	switch {
	case p.VideoCodec != job.VideoH264 && p.VideoCodec != job.VideoHEVC && p.VideoCodec != job.VideoAV1:
		return code.New(code.UnsupportedCodec, field+": video_codec must be h264, hevc or av1", nil)
	case p.PixelFormat != "yuv420p" && p.PixelFormat != "yuv420p10le":
		return code.New(code.UnsupportedCodec, field+": pixel_format must be yuv420p or yuv420p10le", nil)
	case p.PixelFormat == "yuv420p10le" && p.VideoCodec == job.VideoH264:
		return code.New(code.UnsupportedCodec, field+": yuv420p10le needs hevc or av1", nil)
	case !a.validPreset(p.VideoCodec, p.Preset):
		return code.New(code.UnsupportedCodec, field+": preset is not one of the encoder of "+string(p.VideoCodec), nil)
	}

	// the rate control
	maxCRF := 51
	if p.VideoCodec == job.VideoAV1 {
		maxCRF = 63
	}
	switch {
	case p.RateControl != job.RateVBR && p.RateControl != job.RateCRF:
		return code.New(code.InvalidJob, field+": rate_control must be vbr or crf", nil)
	case p.VideoBitrate < 0 || p.VideoBitrate > 100000:
		return code.New(code.InvalidJob, field+": video_bitrate must be within 0..100000 kbps", nil)
	case p.RateControl == job.RateVBR && p.VideoBitrate == 0:
		return code.New(code.InvalidJob, field+": video_bitrate is required for vbr", nil)
	case p.MaxBitrate < 0 || p.MaxBitrate > 200000:
		return code.New(code.InvalidJob, field+": max_bitrate must be within 0..200000 kbps", nil)
	case p.BufferSize < 0 || p.BufferSize > 400000:
		return code.New(code.InvalidJob, field+": buffer_size must be within 0..400000 kbit", nil)
	case p.MaxBitrate > 0 && p.BufferSize == 0:
		return code.New(code.InvalidJob, field+": max_bitrate needs buffer_size", nil)
	case p.BufferSize > 0 && p.MaxBitrate == 0:
		return code.New(code.InvalidJob, field+": buffer_size needs max_bitrate", nil)
	case p.RateControl == job.RateVBR && p.MaxBitrate > 0 && p.MaxBitrate < p.VideoBitrate:
		return code.New(code.InvalidJob, field+": max_bitrate is below video_bitrate", nil)
	case p.CRF < 0 || p.CRF > maxCRF:
		return code.New(code.InvalidJob, fmt.Sprintf("%s: crf must be within 0..%d for %s", field, maxCRF, p.VideoCodec), nil)
	}

	// the audio
	switch {
	case p.SoundCodec != job.SoundAAC:
		return code.New(code.UnsupportedCodec, field+": sound_codec must be aac", nil)
	case p.SoundBitrate < 16 || p.SoundBitrate > 512:
		return code.New(code.InvalidJob, field+": sound_bitrate must be within 16..512 kbps", nil)
	case p.SampleRate != 44100 && p.SampleRate != 48000:
		return code.New(code.InvalidJob, field+": sample_rate must be 44100 or 48000", nil)
	case p.Channels != 1 && p.Channels != 2:
		return code.New(code.InvalidJob, field+": channels must be 1 or 2", nil)
	}
	return nil
}

// checkName — a rendition name: it is a folder in the storage, so 1..32
// letters, digits, "-", "_" or ".", not starting with ".".
func (a *videoCheck) checkName(field, name string) *code.Error {
	switch {
	case name == "":
		return code.New(code.InvalidJob, field+": name is required", nil)
	case len(name) > 32:
		return code.New(code.InvalidJob, field+": name is longer than 32 characters", nil)
	case strings.HasPrefix(name, "."):
		return code.New(code.InvalidJob, field+": name must not start with a dot", nil)
	}

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return code.New(code.InvalidJob, field+": name may hold only letters, digits, - _ and .", nil)
		}
	}
	return nil
}

// checkHLS — the packaging with its defaults: fmp4, 2 second segments, a
// keyframe every segment. Segments are a whole number of keyframe
// intervals, every rendition has a whole number of frames per interval
// (so its keyframes fall on the segment boundaries), MPEG-TS carries only
// H.264, a key is 16 bytes and comes with its uri.
func (a *videoCheck) checkHLS(h *job.HLS, profiles []job.Profile) *code.Error {
	if h.Format == "" {
		h.Format = job.HLSFMP4
	}
	if h.SegmentDuration == 0 {
		h.SegmentDuration = 2
	}
	if h.Keyframe == 0 {
		h.Keyframe = h.SegmentDuration
	}

	// segments and keyframes
	intervals := h.SegmentDuration / h.Keyframe
	switch {
	case h.Format != job.HLSFMP4 && h.Format != job.HLSMPEGTS:
		return code.New(code.InvalidJob, "hls.format must be fmp4 or mpegts", nil)
	case !(h.SegmentDuration >= 1 && h.SegmentDuration <= 10):
		return code.New(code.InvalidJob, "hls.segment_duration must be within 1..10 seconds", nil)
	case !(h.Keyframe > 0 && h.Keyframe <= h.SegmentDuration):
		return code.New(code.InvalidJob, "hls.keyframe must be within 0..segment_duration", nil)
	case math.Abs(intervals-math.Round(intervals)) > 1e-9:
		return code.New(code.InvalidJob, "hls.segment_duration must be a whole number of hls.keyframe", nil)
	}

	// encryption
	switch {
	case len(h.Key) != 0 && len(h.Key) != 16:
		return code.New(code.InvalidJob, "hls.key must be 16 bytes", nil)
	case len(h.Key) != 0 && h.KeyURI == "":
		return code.New(code.InvalidJob, "hls.key_uri is required with hls.key", nil)
	case len(h.Key) == 0 && h.KeyURI != "":
		return code.New(code.InvalidJob, "hls.key_uri is set without hls.key", nil)
	case len(h.KeyURI) > 1024:
		return code.New(code.InvalidJob, "hls.key_uri is longer than 1024 bytes", nil)
	}
	// the key uri: printable ascii only, no spaces and quotes
	for _, c := range []byte(h.KeyURI) {
		switch {
		case c <= ' ', c > '~', c == '"':
			return code.New(code.InvalidJob, "hls.key_uri may hold only printable ascii without quotes and spaces", nil)
		}
	}

	// every rendition against the packaging
	for i, p := range profiles {
		frames := float64(p.FPS) * h.Keyframe
		switch {
		case math.Abs(frames-math.Round(frames)) > 1e-9:
			return code.New(code.InvalidJob, fmt.Sprintf("profiles[%d]: fps × hls.keyframe must be a whole number of frames", i), nil)
		case h.Format == job.HLSMPEGTS && p.VideoCodec != job.VideoH264:
			return code.New(code.UnsupportedCodec, fmt.Sprintf("profiles[%d]: mpegts carries only h264", i), nil)
		}
	}
	return nil
}

// checkThumbnail — one thumbnail with its defaults: jpg, the bee default.
func (a *videoCheck) checkThumbnail(field string, t *job.Thumbnail) *code.Error {
	if t.Format == "" {
		t.Format = job.ImageJPEG
	}
	if t.Quality == 0 {
		t.Quality = a.cfg.Video.ImageQuality
	}

	switch {
	case t.Format != job.ImageJPEG && t.Format != job.ImagePNG && t.Format != job.ImageWebP && t.Format != job.ImageAVIF:
		return code.New(code.UnsupportedCodec, field+": format must be jpg, png, webp or avif", nil)
	case t.Quality < 1 || t.Quality > 100:
		return code.New(code.InvalidJob, field+": quality must be within 1..100", nil)
	case !a.seconds(t.Position):
		return code.New(code.InvalidJob, field+": position must be a non-negative number of seconds", nil)
	case t.Width < 0 || t.Width > a.cfg.Video.MaxDimension:
		return code.New(code.InvalidDimensions, fmt.Sprintf("%s: width must be within 0..%d", field, a.cfg.Video.MaxDimension), nil)
	case t.Height < 0 || t.Height > a.cfg.Video.MaxDimension:
		return code.New(code.InvalidDimensions, fmt.Sprintf("%s: height must be within 0..%d", field, a.cfg.Video.MaxDimension), nil)
	}
	return nil
}

// checkPreview — the preview with its default of 3 seconds and its
// profile.
func (a *videoCheck) checkPreview(p *job.Preview, vertical bool) *code.Error {
	if p.Duration == 0 {
		p.Duration = 3
	}

	switch {
	case !a.seconds(p.Start):
		return code.New(code.InvalidJob, "preview.start must be a non-negative number of seconds", nil)
	case !(p.Duration > 0 && p.Duration <= a.cfg.Video.MaxDuration.Seconds()):
		return code.New(code.InvalidJob, "preview.duration must be within 0..BEE_VIDEO_MAX_DURATION", nil)
	}

	if p.Profile.Name != "" {
		e := a.checkName("preview.profile", p.Profile.Name)
		if e != nil {
			return e
		}
	}
	return a.checkProfile("preview.profile", &p.Profile, vertical)
}

// key — an output key cleaned: one leading "/" trimmed, then a clean
// relative storage key with the extension ext, not used by another field
// (keys maps every key taken to its field).
func (a *videoCheck) key(field, raw, ext string, keys map[string]string) (string, *code.Error) {
	key := strings.TrimPrefix(raw, "/")
	switch {
	case key == "":
		return "", code.New(code.InvalidPath, field+" is required", nil)
	case path.IsAbs(key), key == ".", key != path.Clean(key), strings.Contains(key, ".."):
		return "", code.New(code.InvalidPath, field+" must be a clean relative storage key", nil)
	case strings.ContainsAny(key, " \t\r\n?#\\\"'"):
		return "", code.New(code.InvalidPath, field+" holds a space, quote, ? # or \\", nil)
	case path.Ext(key) != ext:
		return "", code.New(code.InvalidPath, field+" must end with "+ext, nil)
	case keys[key] != "":
		return "", code.New(code.InvalidPath, field+" is the same key as "+keys[key], nil)
	}
	keys[key] = field
	return key, nil
}

// extraKeys — the keys of the thumbnails (the extension of their format)
// and of the preview (.mp4), cleaned in place.
func (a *videoCheck) extraKeys(thumbnails []job.Thumbnail, preview *job.Preview, keys map[string]string) *code.Error {
	var e *code.Error
	for i := range thumbnails {
		t := &thumbnails[i]
		t.Output, e = a.key(fmt.Sprintf("thumbnails[%d].output", i), t.Output, "."+string(t.Format), keys)
		if e != nil {
			return e
		}
	}
	if preview != nil {
		preview.Output, e = a.key("preview.output", preview.Output, ".mp4", keys)
	}
	return e
}

// renditionKeys — no other key lies where the HLS files go: with several
// renditions in the folders <folder of output>/<name>/, with one rendition
// as init.mp4 or segment_* next to the output playlist.
func (a *videoCheck) renditionKeys(output string, profiles []job.Profile, keys map[string]string) *code.Error {
	dir := path.Dir(output)
	for key, field := range keys {
		if key == output {
			continue
		}

		// one rendition: its files next to the playlist
		if len(profiles) == 1 {
			base := path.Base(key)
			switch {
			case path.Dir(key) != dir:
			case base == "init.mp4":
				return code.New(code.InvalidPath, field+" is the init.mp4 of the hls rendition next to output", nil)
			case strings.HasPrefix(base, "segment_"):
				return code.New(code.InvalidPath, field+" is a segment_ file name of the hls rendition next to output", nil)
			}
			continue
		}

		// several: their folders
		for _, p := range profiles {
			folder := path.Join(dir, p.Name)
			switch {
			case key == folder, strings.HasPrefix(key, folder+"/"):
				return code.New(code.InvalidPath, field+" lies in the folder of rendition "+p.Name, nil)
			}
		}
	}
	return nil
}

// color — #rgb or #rrggbb as lowercase #rrggbb.
func (a *videoCheck) color(s string) (string, bool) {
	digits, ok := strings.CutPrefix(s, "#")
	if !ok {
		return "", false
	}
	if len(digits) == 3 {
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	}

	// three bytes of hex
	b, err := hex.DecodeString(digits)
	switch {
	case err != nil:
		return "", false
	case len(b) != 3:
		return "", false
	}
	return "#" + hex.EncodeToString(b), true
}

// validResize — a resize mode of job.go.
func (a *videoCheck) validResize(r job.ResizeMode) bool {
	switch r {
	case job.ResizeCover, job.ResizeContain, job.ResizeStretch:
		return true
	}
	return false
}

// validGravity — a gravity of job.go.
func (a *videoCheck) validGravity(g job.Gravity) bool {
	switch g {
	case job.GravityCenter, job.GravityTop, job.GravityBottom, job.GravityLeft, job.GravityRight,
		job.GravityTopLeft, job.GravityTopRight, job.GravityBottomLeft, job.GravityBottomRight:
		return true
	}
	return false
}

// validPreset — empty (the encoder's own default) or a preset of the
// encoder of the codec: the x264/x265 names (placebo excluded), svt-av1
// 0..13.
func (a *videoCheck) validPreset(c job.VideoCodec, preset string) bool {
	if preset == "" {
		return true
	}
	if c == job.VideoAV1 {
		switch preset {
		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13":
			return true
		}
		return false
	}
	switch preset {
	case "ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow":
		return true
	}
	return false
}

// sameAspect — the frames of two profiles have one aspect ratio within the
// rounding to even pixels (1 %): every rendition is a scaled copy of one
// canvas.
func (a *videoCheck) sameAspect(p, q job.Profile) bool {
	left := float64(p.Width) * float64(q.Height)
	right := float64(q.Width) * float64(p.Height)
	return math.Abs(left-right) <= 0.01*max(left, right)
}

// seconds — x is a finite non-negative number of seconds.
func (a *videoCheck) seconds(x float64) bool {
	return x >= 0 && !math.IsInf(x, 1)
}

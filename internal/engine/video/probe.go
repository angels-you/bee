package video

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Media — what ffprobe reports of a file: the container, the first video
// stream (attached pictures are not video) and the first audio stream.
// Width and Height are the coded frame; DisplayWidth and DisplayHeight the
// frame as it is shown: square pixels (SAR applied) and upright (Rotation
// applied). Bitrates are in kbit/s, 0 when the container does not say.
type Media struct {
	Format   string  // container, as ffprobe names it: mov,mp4,m4a,3gp,3g2,mj2
	Duration float64 // seconds
	Size     int64   // bytes

	HasVideo       bool
	VideoCodec     string // h264, hevc, av1, vp9 …
	Width          int
	Height         int
	SAR            string // sample aspect ratio as ffprobe prints it, 1:1 when square
	DAR            string // display aspect ratio
	DisplayWidth   int
	DisplayHeight  int
	FPS            float64 // average frame rate
	VFR            bool    // the average and the base frame rate differ: variable frame rate
	Rotation       int     // clockwise degrees to show the frame upright: 0, 90, 180, 270
	VideoBitrate   int
	PixelFormat    string // yuv420p, yuv420p10le …
	ColorSpace     string // bt709, bt2020nc …
	ColorTransfer  string // bt709, smpte2084 (PQ), arib-std-b67 (HLG) …
	ColorPrimaries string // bt709, bt2020 …
	HDR            bool   // PQ or HLG transfer: converted to SDR

	HasAudio     bool
	AudioCodec   string
	AudioBitrate int
	SampleRate   int
	Channels     int

	// Codecs — the RFC 6381 codecs of the video and the audio stream,
	// comma separated, as the CODECS attribute of a master playlist wants
	// them (avc1.64001f,mp4a.40.2). Set only when every stream has one:
	// H.264, HEVC or AV1 video, AAC-LC audio.
	Codecs string
}

// probeOutput — ffprobe -show_format -show_streams -show_data -of json.
type probeOutput struct {
	Format  probeFormat   `json:"format"`
	Streams []probeStream `json:"streams"`
}

// probeFormat — the format section of ffprobe.
type probeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
	Size       string `json:"size"`
}

// probeStream — one stream section of ffprobe.
type probeStream struct {
	CodecType      string           `json:"codec_type"`
	CodecName      string           `json:"codec_name"`
	Profile        string           `json:"profile"`
	Width          int              `json:"width"`
	Height         int              `json:"height"`
	SAR            string           `json:"sample_aspect_ratio"`
	DAR            string           `json:"display_aspect_ratio"`
	PixFmt         string           `json:"pix_fmt"`
	ColorSpace     string           `json:"color_space"`
	ColorTransfer  string           `json:"color_transfer"`
	ColorPrimaries string           `json:"color_primaries"`
	AvgFrameRate   string           `json:"avg_frame_rate"`
	RFrameRate     string           `json:"r_frame_rate"`
	BitRate        string           `json:"bit_rate"`
	SampleRate     string           `json:"sample_rate"`
	Channels       int              `json:"channels"`
	Extradata      string           `json:"extradata"`
	Disposition    probeDisposition `json:"disposition"`
	SideData       []probeSideData  `json:"side_data_list"`
}

// probeDisposition — the flags of a stream.
type probeDisposition struct {
	AttachedPic int `json:"attached_pic"`
}

// probeSideData — one side data entry of a stream; the display matrix
// carries the rotation in counter-clockwise degrees.
type probeSideData struct {
	Type     string  `json:"side_data_type"`
	Rotation float64 `json:"rotation"`
}

// ProbeSource — the header of a downloaded source video. An error (not
// readable by a video container demuxer, no stream) is the caller's
// INVALID_VIDEO.
func (a *Tools) ProbeSource(ctx context.Context, file string) (Media, error) {
	return a.probe(ctx, file, a.sourceFormats)
}

// ProbeSound — the header of a downloaded external sound; an error is the
// caller's INVALID_SOUND.
func (a *Tools) ProbeSound(ctx context.Context, file string) (Media, error) {
	return a.probe(ctx, file, a.soundFormats)
}

// ProbePicture — the header of a downloaded watermark picture (png, jpeg,
// webp); an error is the caller's INVALID_WATERMARK.
func (a *Tools) ProbePicture(ctx context.Context, file string) (Media, error) {
	return a.probe(ctx, file, a.pictureFormats)
}

// probe — the header of a local file read with one of the demuxers of
// formats (empty = any: an output bee wrote). A file ffprobe cannot
// read, or one with neither video nor audio, is an error.
func (a *Tools) probe(ctx context.Context, file, formats string) (Media, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return Media{}, err
	}

	// ffprobe reads only the local file, with the allowed demuxers
	args := []string{"-v", "error", "-protocol_whitelist", "file"}
	if formats != "" {
		args = append(args, "-format_whitelist", formats)
	}
	args = append(args, "-show_format", "-show_streams", "-show_data", "-of", "json", "file:"+abs)
	out, _, err := a.run(ctx, "", a.FFprobe, args...)
	if err != nil {
		return Media{}, err
	}

	var p probeOutput
	err = json.Unmarshal(out, &p)
	if err != nil {
		return Media{}, fmt.Errorf("video: ffprobe output: %w", err)
	}

	// the container
	m := Media{Format: p.Format.FormatName}
	m.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	m.Size, _ = strconv.ParseInt(p.Format.Size, 10, 64)

	// the first stream of each kind; a cover picture is not video
	var codecs []string
	for _, s := range p.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && !m.HasVideo:
			m.video(s)
			codecs = append(codecs, s.rfc6381())
		case s.CodecType == "audio" && !m.HasAudio:
			m.audio(s)
			codecs = append(codecs, s.rfc6381())
		}
	}

	switch {
	case !m.HasVideo && !m.HasAudio:
		return Media{}, errors.New("video: no video or audio stream")
	case math.IsNaN(m.Duration), math.IsInf(m.Duration, 0):
		return Media{}, errors.New("video: duration is not a number")
	}

	// the CODECS attribute only when every stream has its codec string
	if !slices.Contains(codecs, "") {
		m.Codecs = strings.Join(codecs, ",")
	}
	return m, nil
}

// AcceptedSource — the video codec of a source is one bee decodes:
// phone and camera codecs, web codecs, ProRes. A fixed property of bee,
// not a setting.
func (a Media) AcceptedSource() bool {
	switch a.VideoCodec {
	case "h264", "hevc", "av1", "vp8", "vp9", "mpeg4", "mpeg2video", "prores":
		return true
	}
	return false
}

// AcceptedSound — the audio codec of an external sound is one bee
// decodes: lossy music formats, lossless, pcm.
func (a Media) AcceptedSound() bool {
	if strings.HasPrefix(a.AudioCodec, "pcm_") {
		return true
	}
	switch a.AudioCodec {
	case "aac", "mp3", "opus", "vorbis", "flac", "alac", "ac3", "eac3":
		return true
	}
	return false
}

// AcceptedPicture — the codec of a watermark is a still picture format:
// png, jpeg, webp.
func (a Media) AcceptedPicture() bool {
	switch a.VideoCodec {
	case "png", "mjpeg", "webp":
		return true
	}
	return false
}

// video — the fields of the video stream s.
func (a *Media) video(s probeStream) {
	a.HasVideo = true
	a.VideoCodec = s.CodecName
	a.Width = s.Width
	a.Height = s.Height
	a.PixelFormat = s.PixFmt
	a.ColorSpace = s.ColorSpace
	a.ColorTransfer = s.ColorTransfer
	a.ColorPrimaries = s.ColorPrimaries
	a.HDR = s.ColorTransfer == "smpte2084" || s.ColorTransfer == "arib-std-b67"
	a.VideoBitrate = s.kbps()
	a.DAR = s.DAR

	// frame rate: the average one; the base one differs for variable
	// frame rate (phones)
	a.FPS = s.ratio(s.AvgFrameRate, "/")
	base := s.ratio(s.RFrameRate, "/")
	if a.FPS <= 0 {
		a.FPS = base
	}
	a.VFR = base > 0 && math.Abs(base-a.FPS) > 0.01

	// square pixels: the shown width is the coded one times the SAR
	a.SAR = s.SAR
	sar := s.ratio(s.SAR, ":")
	if sar <= 0 {
		a.SAR = "1:1"
		sar = 1
	}
	a.DisplayWidth = int(math.Round(float64(s.Width) * sar))
	a.DisplayHeight = s.Height

	// the display matrix turns the frame counter-clockwise; a quarter turn
	// swaps the shown sides
	for _, d := range s.SideData {
		if d.Type != "Display Matrix" {
			continue
		}
		cw := (360 - int(math.Round(d.Rotation))%360) % 360
		if cw%90 == 0 {
			a.Rotation = cw
		}
	}
	if a.Rotation == 90 || a.Rotation == 270 {
		a.DisplayWidth, a.DisplayHeight = a.DisplayHeight, a.DisplayWidth
	}
}

// audio — the fields of the audio stream s.
func (a *Media) audio(s probeStream) {
	a.HasAudio = true
	a.AudioCodec = s.CodecName
	a.AudioBitrate = s.kbps()
	a.SampleRate, _ = strconv.Atoi(s.SampleRate)
	a.Channels = s.Channels
}

// kbps — the bit rate of the stream in kbit/s, rounded; 0 when unknown.
func (a probeStream) kbps() int {
	bps, _ := strconv.ParseFloat(a.BitRate, 64)
	return int(math.Round(bps / 1000))
}

// ratio — "30000/1001" or "4:3" as a number; 0 when it is not one.
func (a probeStream) ratio(value, sep string) float64 {
	num, den, ok := strings.Cut(value, sep)
	if !ok {
		return 0
	}

	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	d, err := strconv.ParseFloat(den, 64)
	if err != nil || d == 0 {
		return 0
	}
	return n / d
}

// extradata — the codec private data of the stream from the hex dump
// ffprobe -show_data prints: "00000000: 0164 001e ffe1 …  .d…", 16 bytes
// a line, the hex part 39 characters wide.
func (a probeStream) extradata() []byte {
	var data []byte
	for _, line := range strings.Split(a.Extradata, "\n") {
		_, dump, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		if len(dump) > 39 {
			dump = dump[:39]
		}

		b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(dump), " ", ""))
		if err != nil {
			return nil
		}
		data = append(data, b...)
	}
	return data
}

// rfc6381 — the codec string of the stream for the CODECS attribute; empty
// for a codec bee does not encode.
func (a probeStream) rfc6381() string {
	switch {
	case a.CodecName == "h264":
		return a.avc()
	case a.CodecName == "hevc":
		return a.hvc()
	case a.CodecName == "av1":
		return a.av01()
	case a.CodecName == "aac" && a.Profile == "LC":
		return "mp4a.40.2"
	}
	return ""
}

// avc — avc1.PPCCLL: profile, constraint flags and level of the SPS, from
// the avcC box (mp4: version byte 1 first) or from the SPS NAL unit of the
// Annex B extradata (MPEG-TS).
func (a probeStream) avc() string {
	data := a.extradata()

	// avcC: 1, profile, constraints, level
	if len(data) >= 4 && data[0] == 1 {
		return fmt.Sprintf("avc1.%02x%02x%02x", data[1], data[2], data[3])
	}

	// Annex B: the bytes after the SPS header (start code 00 00 01, NAL type 7)
	for i := 0; i+6 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 || data[i+2] != 1 {
			continue
		}
		if data[i+3]&0x1f == 7 {
			return fmt.Sprintf("avc1.%02x%02x%02x", data[i+4], data[i+5], data[i+6])
		}
	}
	return ""
}

// hvc — hvc1.<space><profile>.<compat>.<tier><level>[.<constraints>] of
// the hvcC box (ISO/IEC 14496-15 annex E): the compatibility flags in
// reversed bit order as hex, the constraint bytes without the trailing
// zero ones. The sample entry is hvc1 (bee writes -tag:v hvc1).
func (a probeStream) hvc() string {
	data := a.extradata()
	if len(data) < 13 || data[0] != 1 {
		return ""
	}

	// profile space, tier and profile
	space := ""
	switch data[1] >> 6 {
	case 1:
		space = "A"
	case 2:
		space = "B"
	case 3:
		space = "C"
	}
	tier := "L"
	if data[1]&0x20 != 0 {
		tier = "H"
	}
	profile := data[1] & 0x1f

	// compatibility flags, reversed
	compat := uint32(data[2])<<24 | uint32(data[3])<<16 | uint32(data[4])<<8 | uint32(data[5])
	compat = bits.Reverse32(compat)

	s := fmt.Sprintf("hvc1.%s%d.%x.%s%d", space, profile, compat, tier, data[12])

	// constraint bytes up to the last non-zero one
	constraints := data[6:12]
	last := -1
	for i, b := range constraints {
		if b != 0 {
			last = i
		}
	}
	for _, b := range constraints[:last+1] {
		s += fmt.Sprintf(".%x", b)
	}
	return s
}

// av01 — av01.<profile>.<level><tier>.<bit depth> of the av1C box (AV1
// codec ISOBMFF binding).
func (a probeStream) av01() string {
	data := a.extradata()
	if len(data) < 4 || data[0] != 0x81 {
		return ""
	}

	profile := data[1] >> 5
	level := data[1] & 0x1f
	tier := "M"
	if data[2]&0x80 != 0 {
		tier = "H"
	}

	depth := 8
	if data[2]&0x40 != 0 {
		depth = 10
	}
	if data[2]&0x40 != 0 && data[2]&0x20 != 0 {
		depth = 12
	}
	return fmt.Sprintf("av01.%d.%02d%s.%02d", profile, level, tier, depth)
}

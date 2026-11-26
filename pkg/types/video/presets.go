package video

import "math"

// DefaultHLS is optimized for short-form video and CDN delivery.
// With Key set, Bee-style Go encryption uses AES-128-CBC + PKCS7,
// a fresh random 16-byte IV per segment across all profiles and retries,
// and explicit IVs in each media playlist. Leave init.mp4 unencrypted.
func DefaultHLS() HLS {
	return HLS{Format: HLSFMP4, SegmentDuration: 2, Keyframe: 2}
}

// VerticalOptions uses a centered 9:16 crop with no upscaling.
func VerticalOptions() Options {
	return Options{Vertical: true, Resize: ResizeCover, Gravity: GravityCenter}
}

// Quality identifies a vertical 9:16 preset. Every preset is H.264/yuv420p,
// AAC-LC stereo 48 kHz, capped VBR, and 30 FPS (except 1080p60).
// Sizes below assume target bitrates, with audio, in decimal MB (1 MB=1e6 B).
// They exclude container overhead and are estimates, NOT limits.
type Quality string

const (
	Quality240p    Quality = "240p"    // 240x426,   250+64 kbps: 0.39 MB/10s, 2.36 MB/min.
	Quality360p    Quality = "360p"    // 360x640,   500+96 kbps: 0.75 MB/10s, 4.47 MB/min.
	Quality480p    Quality = "480p"    // 480x854,   900+96 kbps: 1.25 MB/10s, 7.47 MB/min.
	Quality540p    Quality = "540p"    // 540x960,  1200+96 kbps: 1.62 MB/10s, 9.72 MB/min.
	Quality720p    Quality = "720p"    // 720x1280, 1800+128 kbps: 2.41 MB/10s, 14.46 MB/min.
	Quality1080p   Quality = "1080p"   // 1080x1920, 3500+128 kbps: 4.54 MB/10s, 27.21 MB/min.
	Quality1080p60 Quality = "1080p60" // 1080x1920, 6000+192 kbps: 7.74 MB/10s, 46.44 MB/min.
)

// Preset returns a fresh profile value, so callers can safely modify it.
// The bitrate ladder is a starting point; validate visual quality on real videos.
func Preset(q Quality) (Profile, bool) {
	p := Profile{
		Name: string(q), FPS: 30,
		VideoCodec: VideoH264, PixelFormat: "yuv420p",
		Preset: "fast", RateControl: RateVBR,
		SoundCodec: SoundAAC, SampleRate: 48000, Channels: 2,
	}

	switch q {
	case Quality240p:
		p.Width, p.Height, p.VideoBitrate, p.SoundBitrate = 240, 426, 250, 64
	case Quality360p:
		p.Width, p.Height, p.VideoBitrate, p.SoundBitrate = 360, 640, 500, 96
	case Quality480p:
		p.Width, p.Height, p.VideoBitrate, p.SoundBitrate = 480, 854, 900, 96
	case Quality540p:
		p.Width, p.Height, p.VideoBitrate, p.SoundBitrate = 540, 960, 1200, 96
	case Quality720p:
		p.Width, p.Height, p.VideoBitrate, p.SoundBitrate = 720, 1280, 1800, 128
	case Quality1080p:
		p.Width, p.Height, p.VideoBitrate, p.SoundBitrate = 1080, 1920, 3500, 128
	case Quality1080p60:
		p.Width, p.Height, p.VideoBitrate, p.SoundBitrate = 1080, 1920, 6000, 192
		p.FPS = 60
	default:
		return Profile{}, false
	}

	// Starting VBV settings: cap peaks at 1.5x target; ~2s of maxrate buffer.
	p.MaxBitrate = p.VideoBitrate * 3 / 2
	p.BufferSize = p.MaxBitrate * 2
	return p, true
}

// VerticalMobile: 360p + 720p. Recommended initial ladder for low storage/CPU.
// ~18.93 MB/min stored for both renditions; a viewer downloads one at a time.
func VerticalMobile() []Profile { return profiles(Quality360p, Quality720p) }

// VerticalStandard: 360p + 720p + 1080p. ~46.14 MB/min stored.
func VerticalStandard() []Profile { return profiles(Quality360p, Quality720p, Quality1080p) }

// VerticalEconomy: 240p + 480p + 720p. ~24.29 MB/min stored.
func VerticalEconomy() []Profile { return profiles(Quality240p, Quality480p, Quality720p) }

// VerticalFull: 240p + 360p + 480p + 720p + 1080p. ~55.97 MB/min stored.
func VerticalFull() []Profile {
	return profiles(Quality240p, Quality360p, Quality480p, Quality720p, Quality1080p)
}

// profiles is used only with known internal Quality constants.
func profiles(qualities ...Quality) []Profile {
	out := make([]Profile, 0, len(qualities))
	for _, q := range qualities {
		p, ok := Preset(q)
		if !ok {
			panic("unknown vertical preset: " + string(q))
		}
		out = append(out, p)
	}
	return out
}

// EstimatedBytes estimates encoded media payload at target bitrates.
// duration is OUTPUT duration, after trim/speed. withAudio=false omits audio.
// Returns 0 when no valid target bitrate/duration is available, including CRF
// profiles without an explicit estimate. Does not include HLS/MP4 overhead.
func (p Profile) EstimatedBytes(duration float64, withAudio bool) int64 {
	// без длительности или битрейта оценки нет
	switch {
	case duration <= 0:
		return 0
	case math.IsNaN(duration):
		return 0
	case math.IsInf(duration, 0):
		return 0
	case p.VideoBitrate <= 0:
		return 0
	}

	bitrate := float64(p.VideoBitrate)
	if withAudio {
		audio := p.SoundBitrate
		if audio <= 0 {
			audio = 128 // Worker default when audio is enabled.
		}
		bitrate += float64(audio)
	}
	size := bitrate * 1000 * duration / 8
	if size >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(math.Ceil(size))
}

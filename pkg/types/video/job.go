// Package video holds the video bee jobs (Stream: HLS, Video: one MP4) and
// their result. No server dependencies: the client imports this package.
// An empty job field means the bee default (env BEE_VIDEO_*) or the value
// given in the field comment; a job does not change the bee limits (size,
// duration, sides).
//
// Jobs and results of the bee video worker.
// All bitrates are in decimal kbit/s, time values are in seconds,
// dimensions are in pixels, and file sizes are in bytes unless noted.
// The json form is the same on every transport (gRPC result json, NATS):
// snake_case keys, HLS.Key as base64.
package video

import "github.com/angels-you/bee/pkg/types/code"

// StreamJob creates HLS VOD. PlaylistPath is the final entry playlist object key.
// One rendition: a media playlist. Multiple renditions: a master playlist
// referencing one media playlist per profile. The same playlists serve all clients.
type StreamJob struct {
	ID           any            `json:"id"`             // Job ID: a string or a number, returned as sent.
	Meta         map[string]any `json:"meta,omitempty"` // Data of the caller, returned untouched in the result.
	Input        string         `json:"input"`          // Presigned HTTPS URL of the source video.
	PlaylistPath string         `json:"playlist_path"`  // Storage key of the entry playlist: video/1111/index.m3u8.
	Options      Options        `json:"options"`
	Profiles     []Profile      `json:"profiles"` // One or more renditions, ordered low to high.
	HLS          HLS            `json:"hls"`
	Thumbnails   []Thumbnail    `json:"thumbnails,omitempty"` // Optional still images.
	Preview      *Preview       `json:"preview,omitempty"`    // Optional short MP4 preview.
}

// VideoJob creates a single progressive MP4 file, without HLS encryption.
type VideoJob struct {
	ID         any            `json:"id"`             // Job ID: a string or a number, returned as sent.
	Meta       map[string]any `json:"meta,omitempty"` // Data of the caller, returned untouched in the result.
	Input      string         `json:"input"`          // Presigned HTTPS URL of the source video.
	FilePath   string         `json:"file_path"`      // Storage key of the MP4: video/1111/video.mp4.
	Options    Options        `json:"options"`
	Profile    Profile        `json:"profile"`
	Thumbnails []Thumbnail    `json:"thumbnails,omitempty"`
	Preview    *Preview       `json:"preview,omitempty"`
}

// Options apply to all output renditions before encoding.
// Zero values disable optional effects unless documented otherwise.
// Processing order: autorotate -> trim -> rotate/mirror -> resize/crop ->
// visual filters -> speed -> audio mix/normalize/fades -> encode.
type Options struct {
	// Video geometry.
	Vertical   bool       `json:"vertical,omitempty"`   // Require a 9:16 target profile (allow even-pixel rounding).
	Resize     ResizeMode `json:"resize,omitempty"`     // Empty = contain, or cover when Vertical is true.
	Gravity    Gravity    `json:"gravity,omitempty"`    // Empty = center.
	Upscale    bool       `json:"upscale,omitempty"`    // Allow increasing source resolution.
	Background string     `json:"background,omitempty"` // Padding color for contain, empty = #000000.
	Rotation   int        `json:"rotation,omitempty"`   // Additional clockwise rotation: 0, 90, 180, 270.
	Mirror     bool       `json:"mirror,omitempty"`     // Horizontal flip.

	// Timeline. Start/Duration refer to the source before Speed.
	Start    float64 `json:"start,omitempty"`    // Skip this many seconds from the beginning.
	Duration float64 `json:"duration,omitempty"` // Maximum source duration; 0 = until EOF.
	Speed    float64 `json:"speed,omitempty"`    // Playback speed, 0 = 1.0; adjust audio tempo as well.

	// Video filters. Nil = unchanged; explicit 0 is meaningful.
	BW         bool     `json:"bw,omitempty"`         // Grayscale output.
	Brightness *float64 `json:"brightness,omitempty"` // FFmpeg eq brightness: -1..1, neutral 0.
	Contrast   *float64 `json:"contrast,omitempty"`   // FFmpeg eq contrast: 0..2, neutral 1.
	Saturation *float64 `json:"saturation,omitempty"` // FFmpeg eq saturation: 0..3, neutral 1.
	Sharpness  *float64 `json:"sharpness,omitempty"`  // Unsharp amount: 0..2, neutral 0.
	Blur       float64  `json:"blur,omitempty"`       // Gaussian blur sigma; 0 = disabled.

	// Audio. NoSound wins over all audio settings.
	Sound          string   `json:"sound,omitempty"`           // Presigned URL of replacement/background audio.
	NoSound        bool     `json:"no_sound,omitempty"`        // Remove every audio track.
	SoundStart     float64  `json:"sound_start,omitempty"`     // Offset into external audio.
	SoundLoop      bool     `json:"sound_loop,omitempty"`      // Loop external audio until video ends.
	SoundVolume    *float64 `json:"sound_volume,omitempty"`    // External volume multiplier; nil = 1, 0 = mute.
	OriginalVolume *float64 `json:"original_volume,omitempty"` // Nil = 0 if Sound set, otherwise 1; 0 = mute.
	Normalize      bool     `json:"normalize,omitempty"`       // Normalize final mix to LUFS / TruePeak.
	LUFS           float64  `json:"lufs,omitempty"`            // Normalize target, -70..-5 LUFS; 0 = BEE_VIDEO_LUFS.
	TruePeak       float64  `json:"true_peak,omitempty"`       // Normalize true peak ceiling, -9..-0.1 dBTP; 0 = BEE_VIDEO_TRUE_PEAK.
	FadeIn         float64  `json:"fade_in,omitempty"`         // Final audio fade-in duration.
	FadeOut        float64  `json:"fade_out,omitempty"`        // Final audio fade-out duration.

	Watermark *Watermark `json:"watermark,omitempty"` // Optional overlay on all renditions.
}

type ResizeMode string

const (
	ResizeCover   ResizeMode = "cover"   // Preserve aspect ratio and crop.
	ResizeContain ResizeMode = "contain" // Preserve aspect ratio and pad.
	ResizeStretch ResizeMode = "stretch" // Force dimensions; may distort image.
)

type Gravity string

const (
	GravityCenter      Gravity = "center"
	GravityTop         Gravity = "top"
	GravityBottom      Gravity = "bottom"
	GravityLeft        Gravity = "left"
	GravityRight       Gravity = "right"
	GravityTopLeft     Gravity = "top-left"
	GravityTopRight    Gravity = "top-right"
	GravityBottomLeft  Gravity = "bottom-left"
	GravityBottomRight Gravity = "bottom-right"
)

type VideoCodec string

const (
	VideoH264 VideoCodec = "h264" // Default: libx264, broad device compatibility.
	VideoHEVC VideoCodec = "hevc" // Optional: libx265, check target clients.
	VideoAV1  VideoCodec = "av1"  // Optional: check encoder and decoder support.
)

type SoundCodec string

const (
	SoundAAC SoundCodec = "aac" // Default: AAC-LC, iOS/Android/Web compatible.
)

type RateControl string

const (
	RateVBR RateControl = "vbr" // Target VideoBitrate, capped by MaxBitrate/BufferSize.
	RateCRF RateControl = "crf" // Quality target; output size is content-dependent.
)

// Profile describes a single output rendition. Width/Height must be even
// for yuv420p. When Upscale=false, skip unsupported ABR renditions (or reject
// a single-file job) rather than silently upscale; report actual dimensions.
type Profile struct {
	Name   string `json:"name"` // Unique rendition directory name, e.g. 720p.
	Width  int    `json:"width"`
	Height int    `json:"height"`
	FPS    int    `json:"fps,omitempty"` // Target constant FPS; 0 = preserve source rate for single MP4.

	VideoCodec   VideoCodec  `json:"video_codec,omitempty"`   // Empty = h264.
	PixelFormat  string      `json:"pixel_format,omitempty"`  // Empty = yuv420p (8-bit SDR).
	Preset       string      `json:"preset,omitempty"`        // Encoder-specific; e.g. libx264 fast.
	RateControl  RateControl `json:"rate_control,omitempty"`  // Empty = vbr.
	VideoBitrate int         `json:"video_bitrate,omitempty"` // Target kbps, not a strict size guarantee.
	MaxBitrate   int         `json:"max_bitrate,omitempty"`   // VBV maxrate, kbps; 0 = not set.
	BufferSize   int         `json:"buffer_size,omitempty"`   // VBV bufsize, kbit; 0 = not set.
	CRF          int         `json:"crf"`                     // Only used in RateCRF; codec-specific, 0 is valid.

	SoundCodec   SoundCodec `json:"sound_codec,omitempty"`   // Empty = aac.
	SoundBitrate int        `json:"sound_bitrate,omitempty"` // AAC target kbps; 0 = BEE_VIDEO_SOUND_BITRATE if audio enabled.
	SampleRate   int        `json:"sample_rate,omitempty"`   // Empty/0 = 48000 Hz.
	Channels     int        `json:"channels,omitempty"`      // Empty/0 = 2 (stereo).
}

type HLSFormat string

const (
	HLSFMP4   HLSFormat = "fmp4"   // Default: fragmented MP4, CMAF-style packaging.
	HLSMPEGTS HLSFormat = "mpegts" // Optional fallback for legacy clients.
)

// HLS controls VOD packaging. For ABR, align segment boundaries, IDR frames,
// frame rates and timestamps across renditions. Use closed GOPs; force a
// keyframe at every segment boundary and avoid independent scene-cut GOPs.
// AES-128 full-segment encryption is an HLS layer, not CMAF CENC/CBCS.
type HLS struct {
	Format          HLSFormat `json:"format,omitempty"`           // Empty = fmp4.
	SegmentDuration float64   `json:"segment_duration,omitempty"` // 0 = 2 seconds.
	Keyframe        float64   `json:"keyframe,omitempty"`         // 0 = SegmentDuration; presets use equal intervals.
	Key             []byte    `json:"key,omitempty"`              // Nil/empty = no encryption; otherwise exactly 16 bytes. Base64 in json.
	KeyURI          string    `json:"key_uri,omitempty"`          // Required with Key: the key uri the player resolves, e.g. skd://1111.
}

// Watermark is an optional transparent image overlay. Opacity nil = 1.
type Watermark struct {
	Input    string   `json:"input"`              // Presigned HTTPS URL (PNG recommended).
	Position Gravity  `json:"position,omitempty"` // Empty = bottom-right.
	Width    int      `json:"width,omitempty"`    // Overlay width in pixels; 0 = BEE_VIDEO_WATERMARK_WIDTH.
	Margin   int      `json:"margin,omitempty"`   // Pixels from the selected edge.
	Opacity  *float64 `json:"opacity,omitempty"`  // 0..1; nil = 1.
}

type ImageFormat string

const (
	ImageJPEG ImageFormat = "jpg"
	ImagePNG  ImageFormat = "png"
	ImageWebP ImageFormat = "webp"
	ImageAVIF ImageFormat = "avif"
)

// Thumbnail extracts a still from the processed timeline, after Speed.
type Thumbnail struct {
	Output   string      `json:"output"`             // S3 object key: video/1111/cover.webp.
	Position float64     `json:"position,omitempty"` // Output timestamp; 0 = first frame.
	Width    int         `json:"width,omitempty"`    // Target bounding box; 0 = preserve source width.
	Height   int         `json:"height,omitempty"`   // Target bounding box; 0 = preserve source height.
	Format   ImageFormat `json:"format,omitempty"`   // Empty = jpg; must match output extension.
	Quality  int         `json:"quality,omitempty"`  // 1..100, 0 = BEE_VIDEO_IMAGE_QUALITY; encoder-specific mapping.
}

// Preview creates a short MP4 from the processed timeline, after Speed.
// It is silent by default, unencrypted, and must not be exposed if the
// corresponding source content is not authorized for preview.
type Preview struct {
	Output   string  `json:"output"`             // S3 object key: video/1111/preview.mp4.
	Start    float64 `json:"start,omitempty"`    // Output timeline offset.
	Duration float64 `json:"duration,omitempty"` // 0 = 3 seconds, bounded by remaining duration.
	Profile  Profile `json:"profile"`
	Sound    bool    `json:"sound,omitempty"` // Include final audio mix; default false.
}

// Result contains measured output metadata (ffprobe + uploaded object sizes).
// Do not include input presigned URLs, encryption keys or credentials.
// A failed job has Error set and no outputs: nothing of it counts,
// whatever is already in the storage.
type Result struct {
	ID           any             `json:"id"`                      // The job ID as sent.
	Meta         map[string]any  `json:"meta,omitempty"`          // The job meta as sent.
	Elapsed      int64           `json:"elapsed"`                 // Milliseconds of the whole job.
	SHA256       string          `json:"sha256,omitempty"`        // Of the source file.
	SourceSize   int64           `json:"source_size,omitempty"`   // Source bytes.
	PlaylistPath string          `json:"playlist_path,omitempty"` // Stream: the entry playlist key, as saved.
	FilePath     string          `json:"file_path,omitempty"`     // Video: the MP4 key, as saved.
	Duration     float64         `json:"duration,omitempty"`      // Processed duration in seconds.
	Size         int64           `json:"size,omitempty"`          // Total published bytes, including auxiliary files.
	Profiles     []ProfileResult `json:"profiles,omitempty"`
	Thumbnails   []ImageResult   `json:"thumbnails,omitempty"`
	Preview      *ProfileResult  `json:"preview,omitempty"`
	Error        *code.Error     `json:"error,omitempty"` // Set only when the job failed.
}

type ProfileResult struct {
	Name         string     `json:"name"`
	Output       string     `json:"output"` // Media playlist or MP4 key.
	Duration     float64    `json:"duration"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	FPS          float64    `json:"fps"`
	VideoCodec   VideoCodec `json:"video_codec"`
	VideoBitrate int        `json:"video_bitrate"` // Measured average kbps, where available.
	SoundCodec   SoundCodec `json:"sound_codec,omitempty"`
	SoundBitrate int        `json:"sound_bitrate,omitempty"` // Measured average kbps, where available.
	HasSound     bool       `json:"has_sound"`
	Size         int64      `json:"size"`     // Rendition bytes including init/media playlist/segments.
	Segments     int        `json:"segments"` // 0 for single MP4.
}

type ImageResult struct {
	Output string      `json:"output"`
	Width  int         `json:"width"`
	Height int         `json:"height"`
	Format ImageFormat `json:"format"`
	Size   int64       `json:"size"`
}

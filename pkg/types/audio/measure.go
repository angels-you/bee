package audio

// Metadata is the technical data and tags of the original from ffprobe.
// Normalized fields are filled only when the tag exists; the raw tags are
// in Tags (lowercase keys, values up to 1024 runes, at most 64)
type Metadata struct {
	DurationMs    int64  `json:"duration_ms"`
	Size          int64  `json:"size"`
	Format        string `json:"format"`                   // container: mp3, flac, wav, mov,mp4,m4a,3gp,3g2,mj2, ogg
	Codec         string `json:"codec"`                    // mp3, flac, pcm_s16le, aac, alac, vorbis, opus
	Bitrate       int    `json:"bitrate,omitempty"`        // bit/s
	SampleRate    int    `json:"sample_rate"`              // Hz
	Channels      int    `json:"channels"`                 //
	ChannelLayout string `json:"channel_layout,omitempty"` // stereo, mono
	BitDepth      int    `json:"bit_depth,omitempty"`      // pcm, flac, alac

	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Composer    string `json:"composer,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Date        string `json:"date,omitempty"` // as in the tag
	Year        int    `json:"year,omitempty"` // from date
	TrackNumber int    `json:"track_number,omitempty"`
	DiscNumber  int    `json:"disc_number,omitempty"`
	ISRC        string `json:"isrc,omitempty"`
	Copyright   string `json:"copyright,omitempty"`
	Encoder     string `json:"encoder,omitempty"`

	Tags map[string]string `json:"tags,omitempty"`

	SHA256 string `json:"sha256,omitempty"` // set by the engine
}

// Loudness is the EBU R128 of the original (ebur128 filter) and of the
// result, and the linear gain between them. Every value is finite: silence
// gets the lower bound instead of -inf (-144 dB level, -70 LUFS loudness)
type Loudness struct {
	Integrated float64 `json:"integrated"`  // LUFS
	TruePeak   float64 `json:"true_peak"`   // dBTP
	SamplePeak float64 `json:"sample_peak"` // dBFS
	LRA        float64 `json:"lra"`         // LU
	Threshold  float64 `json:"threshold"`   // LUFS
	RMS        float64 `json:"rms"`         // dBFS, overall (astats)

	Gain        float64 `json:"gain"`         // dB, linear
	GainLimited bool    `json:"gain_limited"` // the true peak ceiling reduced the gain

	OutputLUFS     float64 `json:"output_lufs"`
	OutputTruePeak float64 `json:"output_true_peak"`
	OutputLRA      float64 `json:"output_lra,omitempty"`
}

// Silence is one stretch of silence, seconds from the start
type Silence struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Duration float64 `json:"duration"`
}

// Quality is the quality diagnostics (Options.Analysis)
type Quality struct {
	Silences        []Silence `json:"silences,omitempty"`
	LeadingSilence  float64   `json:"leading_silence"`  // seconds of silence at the start
	TrailingSilence float64   `json:"trailing_silence"` // seconds of silence at the end

	Clipping   bool    `json:"clipping"`    // a full scale peak and flat factor above 0 dB
	FlatFactor float64 `json:"flat_factor"` // astats, dB: runs of equal samples at min / max

	ChannelRMS       []float64 `json:"channel_rms,omitempty"` // dBFS per channel
	ChannelImbalance float64   `json:"channel_imbalance"`     // dB between the loudest and the quietest channel
	DCOffset         float64   `json:"dc_offset"`             // overall
	DynamicRange     float64   `json:"dynamic_range"`         // sample peak - rms, dB (crest factor)
}

// Fingerprint is the chromaprint of the original (Options.Fingerprint)
type Fingerprint struct {
	Algorithm   string  `json:"algorithm"` // chromaprint
	Version     string  `json:"version"`   // fpcalc version
	Fingerprint string  `json:"fingerprint"`
	Duration    float64 `json:"duration"` // seconds analyzed
	MaxSeconds  int     `json:"max_seconds"`
}

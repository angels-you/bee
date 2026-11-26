package audio

// Metadata — технические данные и теги оригинала из ffprobe.
// Нормализованные поля заполнены, только если тег есть; исходные теги —
// в Tags (ключи в нижнем регистре, значения до 1024 рун, не больше 64)
type Metadata struct {
	DurationMs    int64  `json:"duration_ms"`
	Size          int64  `json:"size"`
	Format        string `json:"format"`                   // контейнер: mp3, flac, wav, mov,mp4,m4a,3gp,3g2,mj2, ogg
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
	Date        string `json:"date,omitempty"` // как в теге
	Year        int    `json:"year,omitempty"` // из date
	TrackNumber int    `json:"track_number,omitempty"`
	DiscNumber  int    `json:"disc_number,omitempty"`
	ISRC        string `json:"isrc,omitempty"`
	Copyright   string `json:"copyright,omitempty"`
	Encoder     string `json:"encoder,omitempty"`

	Tags map[string]string `json:"tags,omitempty"`

	SHA256 string `json:"sha256,omitempty"` // ставит движок
}

// Loudness — EBU R128 оригинала (фильтр ebur128) и результата, и
// линейное усиление между ними. Каждое значение конечное: вместо -inf
// тишины пишется нижняя граница (-144 dB уровня, -70 LUFS громкости)
type Loudness struct {
	Integrated float64 `json:"integrated"`  // LUFS
	TruePeak   float64 `json:"true_peak"`   // dBTP
	SamplePeak float64 `json:"sample_peak"` // dBFS
	LRA        float64 `json:"lra"`         // LU
	Threshold  float64 `json:"threshold"`   // LUFS
	RMS        float64 `json:"rms"`         // dBFS, общий (astats)

	Gain        float64 `json:"gain"`         // dB, линейно
	GainLimited bool    `json:"gain_limited"` // потолок true peak уменьшил усиление

	OutputLUFS     float64 `json:"output_lufs"`
	OutputTruePeak float64 `json:"output_true_peak"`
	OutputLRA      float64 `json:"output_lra,omitempty"`
}

// Silence — один отрезок тишины, секунды от начала
type Silence struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Duration float64 `json:"duration"`
}

// Quality — диагностика качества (Options.Analysis)
type Quality struct {
	Silences        []Silence `json:"silences,omitempty"`
	LeadingSilence  float64   `json:"leading_silence"`  // секунд тишины в начале
	TrailingSilence float64   `json:"trailing_silence"` // секунд тишины в конце

	Clipping   bool    `json:"clipping"`    // пик на полной шкале и flat factor выше 0 dB
	FlatFactor float64 `json:"flat_factor"` // astats, dB: серии одинаковых сэмплов на min / max

	ChannelRMS       []float64 `json:"channel_rms,omitempty"` // dBFS по каналам
	ChannelImbalance float64   `json:"channel_imbalance"`     // dB между самым громким и самым тихим каналом
	DCOffset         float64   `json:"dc_offset"`             // общий
	DynamicRange     float64   `json:"dynamic_range"`         // sample peak - rms, dB (crest factor)
}

// Fingerprint — chromaprint оригинала (Options.Fingerprint)
type Fingerprint struct {
	Algorithm   string  `json:"algorithm"` // chromaprint
	Version     string  `json:"version"`   // версия fpcalc
	Fingerprint string  `json:"fingerprint"`
	Duration    float64 `json:"duration"` // секунд проанализировано
	MaxSeconds  int     `json:"max_seconds"`
}

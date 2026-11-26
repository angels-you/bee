package config

import (
	"errors"
	"fmt"
	"time"
)

// Audio holds audio job limits and defaults for empty audio.Options fields
type Audio struct {

	// server limits: a job cannot change them
	Timeout     time.Duration `env:"TIMEOUT"`       // one job, from download to upload
	MaxSourceMB int64         `env:"MAX_SOURCE_MB"` // largest original, MiB
	MinDuration time.Duration `env:"MIN_DURATION"`  // shortest original
	MaxDuration time.Duration `env:"MAX_DURATION"`  // longest original

	// processing defaults: an empty job field = this value
	LUFS           float64 `env:"LUFS"`            // loudness target, LUFS: below 0 and above -70
	TruePeak       float64 `env:"TRUE_PEAK"`       // true peak ceiling, dBTP: not above 0
	Renditions     []int   `env:"RENDITIONS"`      // AAC bitrates of the HLS renditions, kbit/s, in master playlist order
	SampleRate     int     `env:"SAMPLE_RATE"`     // Hz of each rendition
	Channels       int     `env:"CHANNELS"`        // channels of each rendition
	Segment        int     `env:"SEGMENT"`         // HLS segment length, seconds
	WaveformPPS    int     `env:"WAVEFORM_PPS"`    // points per second in the waveform json
	WaveformPoints int     `env:"WAVEFORM_POINTS"` // points in Result.Waveform
	Analysis       bool    `env:"ANALYSIS"`        // quality diagnostics (astats, silencedetect)
	Fingerprint    bool    `env:"FINGERPRINT"`     // chromaprint via fpcalc, only in Result
	SilenceDB      float64 `env:"SILENCE_DB"`      // silence threshold, dB below 0
	SilenceSec     float64 `env:"SILENCE_SEC"`     // shortest silence in the report, seconds
}

// defaults are values proven in production
func (a *Audio) defaults() {
	a.Timeout = 5 * time.Minute
	a.MaxSourceMB = 500
	a.MinDuration = time.Second
	a.MaxDuration = 30 * time.Minute

	a.LUFS = -14
	a.TruePeak = -1.5
	a.Renditions = []int{128, 64, 256}
	a.SampleRate = 44100
	a.Channels = 2
	a.Segment = 6
	a.WaveformPPS = 100
	a.WaveformPoints = 512
	a.Analysis = false
	a.Fingerprint = false
	a.SilenceDB = -60
	a.SilenceSec = 2
}

// validate checks that all fields are within their limits
func (a *Audio) validate(download time.Duration) (errs []error) {

	// limits
	if a.Timeout <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_TIMEOUT must be positive"))
	}

	if download > a.Timeout {
		errs = append(errs, errors.New("config: BEE_DOWNLOAD_TIMEOUT must be at most BEE_AUDIO_TIMEOUT"))
	}

	if a.MaxSourceMB <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_MAX_SOURCE_MB must be positive"))
	}

	if a.MinDuration <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_MIN_DURATION must be positive"))
	}

	if a.MaxDuration <= a.MinDuration {
		errs = append(errs, errors.New("config: BEE_AUDIO_MAX_DURATION must be above BEE_AUDIO_MIN_DURATION"))
	}

	// loudness
	if !(a.LUFS < 0 && a.LUFS > -70) {
		errs = append(errs, errors.New("config: BEE_AUDIO_LUFS must be below 0 and above -70"))
	}

	if a.TruePeak > 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_TRUE_PEAK must be at most 0"))
	}

	// renditions: a rendition folder is its bitrate, so they must be distinct and positive
	if len(a.Renditions) == 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_RENDITIONS is required"))
	}

	seen := map[int]bool{}
	for _, kbit := range a.Renditions {
		switch {
		case kbit <= 0:
			errs = append(errs, fmt.Errorf("config: BEE_AUDIO_RENDITIONS: %d is not a positive kbit/s", kbit))
		case seen[kbit]:
			errs = append(errs, fmt.Errorf("config: BEE_AUDIO_RENDITIONS: %d is used twice", kbit))
		}
		seen[kbit] = true
	}

	// format
	if a.SampleRate <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_SAMPLE_RATE must be positive"))
	}

	if a.Channels <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_CHANNELS must be positive"))
	}

	if a.Segment <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_SEGMENT must be positive"))
	}

	// waveform
	if a.WaveformPPS <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_WAVEFORM_PPS must be positive"))
	}

	if a.WaveformPoints <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_WAVEFORM_POINTS must be positive"))
	}

	// silence
	if a.SilenceDB >= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_SILENCE_DB must be below 0"))
	}

	if a.SilenceSec <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_SILENCE_SEC must be positive"))
	}

	return
}

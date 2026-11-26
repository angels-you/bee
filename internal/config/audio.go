package config

import (
	"errors"
	"fmt"
	"time"
)

// Audio — лимиты аудио job и дефолты для пустых полей audio.Options
type Audio struct {

	// лимиты сервера: job их не меняет
	Timeout     time.Duration `env:"TIMEOUT"`       // одна job, от скачивания до загрузки
	MaxSourceMB int64         `env:"MAX_SOURCE_MB"` // самый большой оригинал, MiB
	MinDuration time.Duration `env:"MIN_DURATION"`  // самый короткий оригинал
	MaxDuration time.Duration `env:"MAX_DURATION"`  // самый длинный оригинал

	// дефолты обработки: пустое поле job = это значение
	LUFS           float64 `env:"LUFS"`            // цель громкости, LUFS: ниже 0 и выше -70
	TruePeak       float64 `env:"TRUE_PEAK"`       // потолок true peak, dBTP: не выше 0
	Renditions     []int   `env:"RENDITIONS"`      // битрейты AAC вариантов HLS, kbit/s, в порядке master playlist
	SampleRate     int     `env:"SAMPLE_RATE"`     // Hz каждого варианта
	Channels       int     `env:"CHANNELS"`        // каналов каждого варианта
	Segment        int     `env:"SEGMENT"`         // длина сегмента HLS, секунд
	WaveformPPS    int     `env:"WAVEFORM_PPS"`    // точек в секунду в waveform json
	WaveformPoints int     `env:"WAVEFORM_POINTS"` // точек в Result.Waveform
	Analysis       bool    `env:"ANALYSIS"`        // диагностика качества (astats, silencedetect)
	Fingerprint    bool    `env:"FINGERPRINT"`     // chromaprint через fpcalc, только в Result
	SilenceDB      float64 `env:"SILENCE_DB"`      // порог тишины, dB ниже 0
	SilenceSec     float64 `env:"SILENCE_SEC"`     // самая короткая тишина в отчёте, секунд
}

// defaults — значения из рабочего bee
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

// validate — все поля в своих пределах
func (a *Audio) validate(download time.Duration) (errs []error) {

	// лимиты
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

	// громкость
	if !(a.LUFS < 0 && a.LUFS > -70) {
		errs = append(errs, errors.New("config: BEE_AUDIO_LUFS must be below 0 and above -70"))
	}

	if a.TruePeak > 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_TRUE_PEAK must be at most 0"))
	}

	// варианты: папка варианта — его битрейт, поэтому разные и положительные
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

	// формат
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

	// тишина
	if a.SilenceDB >= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_SILENCE_DB must be below 0"))
	}

	if a.SilenceSec <= 0 {
		errs = append(errs, errors.New("config: BEE_AUDIO_SILENCE_SEC must be positive"))
	}

	return
}

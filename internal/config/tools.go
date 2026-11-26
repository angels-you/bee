package config

import "errors"

// Tools — бинарники обработки: путь или имя в PATH. Образ docker ставит
// все пять
type Tools struct {
	FFmpeg     string `env:"FFMPEG"`     // аудио и видео
	FFprobe    string `env:"FFPROBE"`    // аудио и видео
	Fpcalc     string `env:"FPCALC"`     // chromaprint для fingerprint аудио; пусто — нет
	Vips       string `env:"VIPS"`       // картинки
	VipsHeader string `env:"VIPSHEADER"` // картинки
}

// defaults — имена в PATH
func (a *Tools) defaults() {
	a.FFmpeg = "ffmpeg"
	a.FFprobe = "ffprobe"
	a.Fpcalc = "fpcalc"
	a.Vips = "vips"
	a.VipsHeader = "vipsheader"
}

// validate — все бинарники заданы; fpcalc нужен, если fingerprint
// включён по умолчанию
func (a *Tools) validate(fingerprint bool) (errs []error) {
	if a.FFmpeg == "" {
		errs = append(errs, errors.New("config: BEE_FFMPEG is required"))
	}

	if a.FFprobe == "" {
		errs = append(errs, errors.New("config: BEE_FFPROBE is required"))
	}

	if a.Vips == "" {
		errs = append(errs, errors.New("config: BEE_VIPS is required"))
	}

	if a.VipsHeader == "" {
		errs = append(errs, errors.New("config: BEE_VIPSHEADER is required"))
	}

	// fingerprint по умолчанию без fpcalc невозможен
	if fingerprint {
		if a.Fpcalc == "" {
			errs = append(errs, errors.New("config: BEE_AUDIO_FINGERPRINT needs BEE_FPCALC"))
		}
	}

	return
}

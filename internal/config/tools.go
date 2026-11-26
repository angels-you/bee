package config

import "errors"

// Tools are the processing binaries: a path or a name in PATH. The docker image
// installs all five
type Tools struct {
	FFmpeg     string `env:"FFMPEG"`     // audio and video
	FFprobe    string `env:"FFPROBE"`    // audio and video
	Fpcalc     string `env:"FPCALC"`     // chromaprint for audio fingerprint; empty means none
	Vips       string `env:"VIPS"`       // images
	VipsHeader string `env:"VIPSHEADER"` // images
}

// defaults are names in PATH
func (a *Tools) defaults() {
	a.FFmpeg = "ffmpeg"
	a.FFprobe = "ffprobe"
	a.Fpcalc = "fpcalc"
	a.Vips = "vips"
	a.VipsHeader = "vipsheader"
}

// validate checks that all binaries are set; fpcalc is required if fingerprint
// is enabled by default
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

	// fingerprint by default is impossible without fpcalc
	if fingerprint {
		if a.Fpcalc == "" {
			errs = append(errs, errors.New("config: BEE_AUDIO_FINGERPRINT needs BEE_FPCALC"))
		}
	}

	return
}

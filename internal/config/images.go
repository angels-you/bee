package config

import (
	"errors"
	"time"
)

// Images holds image limits and defaults for empty images.Profile fields
type Images struct {

	// server limits: a job cannot change them
	Timeout       time.Duration `env:"TIMEOUT"`        // one job, from download to upload
	MaxSourceMB   int64         `env:"MAX_SOURCE_MB"`  // largest original, MiB
	MaxMegapixels int           `env:"MAX_MEGAPIXELS"` // original width × height, millions
	MaxDimension  int           `env:"MAX_DIMENSION"`  // longest side of the result (and of the task box), px
	Threads       int           `env:"THREADS"`        // threads of one vips run

	// processing defaults: an empty job field = this value
	MinQuality int `env:"MIN_QUALITY"` // max_bytes does not lower quality below this, 1..100
	AvifEffort int `env:"AVIF_EFFORT"` // avif encoder effort, 0..9: higher is slower and smaller
}

// defaults are values proven in production
func (a *Images) defaults() {
	a.Timeout = 2 * time.Minute
	a.MaxSourceMB = 50
	a.MaxMegapixels = 100
	a.MaxDimension = 8000
	a.Threads = 4

	a.MinQuality = 30
	a.AvifEffort = 4
}

// validate checks that all fields are within their limits
func (a *Images) validate(download time.Duration) (errs []error) {

	// limits
	if a.Timeout <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_TIMEOUT must be positive"))
	}

	if download > a.Timeout {
		errs = append(errs, errors.New("config: BEE_DOWNLOAD_TIMEOUT must be at most BEE_IMAGES_TIMEOUT"))
	}

	if a.MaxSourceMB <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_MAX_SOURCE_MB must be positive"))
	}

	if a.MaxMegapixels <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_MAX_MEGAPIXELS must be positive"))
	}

	if a.MaxDimension <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_MAX_DIMENSION must be positive"))
	}

	if a.Threads < 1 {
		errs = append(errs, errors.New("config: BEE_IMAGES_THREADS must be at least 1"))
	}

	// processing defaults
	if !(a.MinQuality >= 1 && a.MinQuality <= 100) {
		errs = append(errs, errors.New("config: BEE_IMAGES_MIN_QUALITY must be within 1..100"))
	}

	if !(a.AvifEffort >= 0 && a.AvifEffort <= 9) {
		errs = append(errs, errors.New("config: BEE_IMAGES_AVIF_EFFORT must be within 0..9"))
	}

	return
}

// Package config holds bee settings from env (envy, prefix BEE_). Only
// the storage (BEE_STORAGE and the keys of the chosen kind) and at least one
// transport (BEE_HTTP_ADDR, BEE_GRPC_ADDR or BEE_NATS_URL) are required,
// and BEE_TOKEN with http or grpc;
// everything else has defaults (defaults), all of them shown in cmd/.env.
//
// A job cannot change server limits (size, duration, concurrency, timeout);
// processing parameters here are only defaults for empty job fields
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monopolly/envy"
)

// Config holds all bee settings
type Config struct {
	WorkerID          string        `env:"WORKER_ID"`             // bee name in logs and in nats
	TempDir           string        `env:"TEMP_DIR"`              // temporary job folders are created here
	Concurrency       int           `env:"CONCURRENCY"`           // jobs at once, all types together, at least 1
	QueueWait         time.Duration `env:"QUEUE_WAIT"`            // how long a job waits for a free slot, 0 rejects at once
	DownloadTimeout   time.Duration `env:"DOWNLOAD_TIMEOUT"`      // download of all urls of one job, at most the type Timeout
	UploadConcurrency int           `env:"UPLOAD_CONCURRENCY"`    // objects of one job uploaded at once
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT"`      // how long running jobs get on shutdown
	Token             string        `env:"TOKEN"`                 // http and grpc token; required with either of them
	SourceHosts       []string      `env:"SOURCE_HOSTS"`          // allowed url hosts (by suffix); empty means any public host
	AllowInsecure     bool          `env:"ALLOW_INSECURE_SOURCE"` // http and private addresses: development and tests only

	Tools   Tools
	Storage Storage
	Audio   Audio  `prefix:"AUDIO_"`
	Video   Video  `prefix:"VIDEO_"`
	Images  Images `prefix:"IMAGES_"`
	Health  Health `prefix:"HEALTH_"`
	HTTP    HTTP   `prefix:"HTTP_"`
	GRPC    GRPC   `prefix:"GRPC_"`
	NATS    NATS   `prefix:"NATS_"`
}

// Load applies defaults, then .env files (if given; the real env wins over
// the file), then env BEE_*, and validates. The error names every invalid
// field
func Load(files ...string) (*Config, error) {
	a := &Config{}
	a.defaults()

	// .env files
	for _, name := range files {
		err := envy.File(name)
		if err != nil {
			return nil, err
		}
	}

	// env over defaults: an unset key leaves the default alone
	env := struct {
		Bee *Config `prefix:"BEE_"`
	}{Bee: a}

	err := envy.Parse(&env)
	if err != nil {
		return nil, err
	}

	err = a.validate()
	if err != nil {
		return nil, err
	}

	return a, nil
}

// defaults returns the values of all optional keys; cmd/.env shows the same
// values. Storage and transports have no defaults
func (a *Config) defaults() {
	a.WorkerID = "bee-1"
	a.TempDir = "/tmp/bee"
	a.Concurrency = 2
	a.QueueWait = 0
	a.DownloadTimeout = 2 * time.Minute
	a.UploadConcurrency = 8
	a.ShutdownTimeout = 2 * time.Minute
	a.Token = ""
	a.SourceHosts = nil
	a.AllowInsecure = false

	a.Tools.defaults()
	a.Storage.defaults()
	a.Audio.defaults()
	a.Video.defaults()
	a.Images.defaults()
	a.NATS.defaults()
	a.Health.defaults()
}

// validate checks that all fields are within their limits; all errors at once,
// one line per field
func (a *Config) validate() error {
	var errs []error

	errs = append(errs, a.validateCommon()...)
	errs = append(errs, a.Tools.validate(a.Audio.Fingerprint)...)
	errs = append(errs, a.Storage.validate()...)
	errs = append(errs, a.Audio.validate(a.DownloadTimeout)...)
	errs = append(errs, a.Video.validate(a.DownloadTimeout)...)
	errs = append(errs, a.Images.validate(a.DownloadTimeout)...)
	errs = append(errs, a.validateTransports()...)

	return errors.Join(errs...)
}

// validateCommon checks the common fields
func (a *Config) validateCommon() (errs []error) {
	if a.WorkerID == "" {
		errs = append(errs, errors.New("config: BEE_WORKER_ID is required"))
	}

	if a.TempDir == "" {
		errs = append(errs, errors.New("config: BEE_TEMP_DIR is required"))
	}

	if a.Concurrency < 1 {
		errs = append(errs, errors.New("config: BEE_CONCURRENCY must be at least 1"))
	}

	if a.QueueWait < 0 {
		errs = append(errs, errors.New("config: BEE_QUEUE_WAIT must not be negative"))
	}

	if a.DownloadTimeout <= 0 {
		errs = append(errs, errors.New("config: BEE_DOWNLOAD_TIMEOUT must be positive"))
	}

	if a.UploadConcurrency < 1 {
		errs = append(errs, errors.New("config: BEE_UPLOAD_CONCURRENCY must be at least 1"))
	}

	if a.ShutdownTimeout < 0 {
		errs = append(errs, errors.New("config: BEE_SHUTDOWN_TIMEOUT must not be negative"))
	}

	// hosts: no empty ones and no spaces
	for _, h := range a.SourceHosts {
		switch {
		case h == "":
			errs = append(errs, errors.New("config: BEE_SOURCE_HOSTS: empty host"))
		case h != strings.TrimSpace(h):
			errs = append(errs, fmt.Errorf("config: BEE_SOURCE_HOSTS: %q is not a host", h))
		}
	}

	return
}

// validateTransports checks that at least one transport is enabled and an
// enabled nats is within its limits
func (a *Config) validateTransports() (errs []error) {
	if a.transports() == 0 {
		errs = append(errs, errors.New("config: no transport: set BEE_HTTP_ADDR, BEE_GRPC_ADDR or BEE_NATS_URL"))
	}

	if a.NATS.Enabled() {
		errs = append(errs, a.NATS.validate()...)
	}

	// http and grpc are never open: they need the token
	if a.HTTP.Enabled() || a.GRPC.Enabled() {
		if a.Token == "" {
			errs = append(errs, errors.New("config: BEE_TOKEN is required with BEE_HTTP_ADDR or BEE_GRPC_ADDR"))
		}
	}

	// the health server always runs
	if a.Health.Addr == "" {
		errs = append(errs, errors.New("config: BEE_HEALTH_ADDR is required"))
	}

	return
}

// SourceHostAllowed reports whether host is one of SourceHosts or under one;
// any host if the list is empty. For storage.Downloader.HostAllowed
func (a *Config) SourceHostAllowed(host string) bool {
	if len(a.SourceHosts) == 0 {
		return true
	}

	host = strings.ToLower(host)
	for _, h := range a.SourceHosts {
		h = strings.ToLower(h)

		switch {
		case host == h:
			return true
		case strings.HasSuffix(host, "."+h):
			return true
		}
	}

	return false
}

// transports returns how many transports are enabled
func (a *Config) transports() (n int) {
	if a.HTTP.Enabled() {
		n++
	}

	if a.GRPC.Enabled() {
		n++
	}

	if a.NATS.Enabled() {
		n++
	}

	return
}

// Defaults returns a config with defaults only, no env and no validation: a base
// for tests, storage and transports are not set
func Defaults() *Config {
	a := &Config{}
	a.defaults()

	return a
}

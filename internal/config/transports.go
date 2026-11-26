package config

import (
	"errors"
	"fmt"
	"time"
)

// HTTP receives jobs over http, serves health and metrics; empty Addr disables it
type HTTP struct {
	Addr string `env:"ADDR"` // :8080
}

// Enabled reports whether the transport is enabled
func (a *HTTP) Enabled() bool {
	return a.Addr != ""
}

// GRPC receives jobs over grpc; empty Addr disables it
type GRPC struct {
	Addr string `env:"ADDR"` // :9090
}

// Enabled reports whether the transport is enabled
func (a *GRPC) Enabled() bool {
	return a.Addr != ""
}

// NATS receives jobs from NATS JetStream; empty URL disables it. Name is the
// stream, subjects and consumer name of this bee: bees with different buckets
// on the same nats must use different names
type NATS struct {
	URL  string `env:"URL"`  // nats://host:4222, credentials inside the url
	Name string `env:"NAME"` // stream BEE, subjects bee.audio.new etc., consumer bee

	// AckWait is how long a delivery lives without signs of life before
	// JetStream hands it to another bee; a running job sends progress every
	// AckWait / 3
	AckWait time.Duration `env:"ACK_WAIT"`

	// MaxDeliver is the number of deliveries of one job: the last one's error is final
	MaxDeliver    int           `env:"MAX_DELIVER"`
	RetryDelay    time.Duration `env:"RETRY_DELAY"`     // nak delay after the first delivery, doubled afterwards
	RetryMaxDelay time.Duration `env:"RETRY_MAX_DELAY"` // longest nak delay

	// stream and result log: work queue, a message lives until ack
	Storage  string        `env:"STORAGE"`  // file or memory
	Replicas int           `env:"REPLICAS"` // 1..5, at most the number of cluster servers
	MaxAge   time.Duration `env:"MAX_AGE"`  // an unprocessed message is deleted after this time; 0 means never

	// ResultTTL is how long the log keeps the result of a finished job: longer than
	// a job may wait for redelivery
	ResultTTL time.Duration `env:"RESULT_TTL"`
}

// Enabled reports whether the transport is enabled
func (a *NATS) Enabled() bool {
	return a.URL != ""
}

// defaults are values proven in production
func (a *NATS) defaults() {
	a.Name = "bee"
	a.AckWait = time.Minute
	a.MaxDeliver = 5
	a.RetryDelay = 10 * time.Second
	a.RetryMaxDelay = 5 * time.Minute
	a.Storage = "file"
	a.Replicas = 1
	a.MaxAge = 0
	a.ResultTTL = 24 * time.Hour
}

// validate checks that the enabled nats fields are within their limits
func (a *NATS) validate() (errs []error) {
	// the name goes into stream names and subjects: a-z, 0-9, _ and -
	for _, r := range a.Name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			errs = append(errs, fmt.Errorf("config: BEE_NATS_NAME %q may hold only a-z, 0-9, _ and -", a.Name))
			return
		}
	}

	if a.Name == "" {
		errs = append(errs, errors.New("config: BEE_NATS_NAME is required"))
	}

	if a.AckWait <= 0 {
		errs = append(errs, errors.New("config: BEE_NATS_ACK_WAIT must be positive"))
	}

	if a.MaxDeliver < 1 {
		errs = append(errs, errors.New("config: BEE_NATS_MAX_DELIVER must be at least 1"))
	}

	if a.RetryDelay <= 0 {
		errs = append(errs, errors.New("config: BEE_NATS_RETRY_DELAY must be positive"))
	}

	if a.RetryMaxDelay < a.RetryDelay {
		errs = append(errs, errors.New("config: BEE_NATS_RETRY_MAX_DELAY must be at least BEE_NATS_RETRY_DELAY"))
	}

	switch a.Storage {
	case "file", "memory":
	default:
		errs = append(errs, errors.New("config: BEE_NATS_STORAGE must be file or memory"))
	}

	if !(a.Replicas >= 1 && a.Replicas <= 5) {
		errs = append(errs, errors.New("config: BEE_NATS_REPLICAS must be within 1..5"))
	}

	if a.MaxAge < 0 {
		errs = append(errs, errors.New("config: BEE_NATS_MAX_AGE must not be negative"))
	}

	if a.ResultTTL <= 0 {
		errs = append(errs, errors.New("config: BEE_NATS_RESULT_TTL must be positive"))
	}

	return
}

// Health is the health server: liveness, readiness and metrics, always
// on, without a token.
type Health struct {
	Addr string `env:"ADDR"` // :8081
}

// defaults is the port of the health server.
func (a *Health) defaults() {
	a.Addr = ":8081"
}

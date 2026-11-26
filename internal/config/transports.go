package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/angels-you/bee/pkg/proto"
)

// HTTP receives jobs over http (token required); empty Addr disables it
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

// NATS receives jobs from NATS JetStream; empty URL disables it. Jobs and
// Done are the subject bases: jobs come on <jobs>.<kind>, results go to
// <done>.<kind>.<id>; the stream and consumer names are derived
// from them (proto.Names). Bees with the same bases share one queue; bees
// with other buckets on the same nats need other bases
type NATS struct {
	URL   string `env:"URL"`   // nats://host:4222
	Token string `env:"TOKEN"` // nats auth token; empty: none
	Jobs  string `env:"JOBS"`  // subject base of the jobs
	Done  string `env:"DONE"`  // subject base of the results

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

// Names are the subjects and stream names of the bases
func (a *NATS) Names() proto.Names {
	return proto.Names{Jobs: a.Jobs, Done: a.Done}
}

// Enabled reports whether the transport is enabled
func (a *NATS) Enabled() bool {
	return a.URL != ""
}

// defaults are values proven in production
func (a *NATS) defaults() {
	// the subject bases: one public place for bee and its clients
	names := proto.Names{}
	a.Jobs = names.JobsBase()
	a.Done = names.DoneBase()

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
	// the subject bases: set, valid, apart
	switch {
	case a.Jobs == "":
		errs = append(errs, errors.New("config: BEE_NATS_JOBS is required"))
	case a.Done == "":
		errs = append(errs, errors.New("config: BEE_NATS_DONE is required"))
	default:
		err := a.Names().Validate()
		if err != nil {
			errs = append(errs, fmt.Errorf("config: BEE_NATS_JOBS, BEE_NATS_DONE: %w", err))
		}
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

// Package proto is the bee protocol shared by servers and clients:
// bee.proto and the generated gRPC code, job types (Kind) and transport
// names (Names): http paths, gRPC trailers, nats subjects and streams.
//
// On every transport the job and the result are json of the public types
// github.com/angels-you/bee/pkg/types/audio, video, images
package proto

import (
	"errors"
	"fmt"
	"strings"
)

// Kind is a job type
type Kind string

const (
	Audio  Kind = "audio"  // audio.Job → audio.Result
	Stream Kind = "stream" // video.Stream → video.Result
	Video  Kind = "video"  // video.Video → video.Result
	Images Kind = "images" // images.Job → images.Result
)

// Kinds returns all job types
func Kinds() []Kind {
	return []Kind{Audio, Stream, Video, Images}
}

// Valid reports whether the type is known
func (a Kind) Valid() bool {
	switch a {
	case Audio, Stream, Video, Images:
		return true
	}

	return false
}

// Names are the transport names of one group of bees. Jobs and Done are
// the two nats subjects (BEE_NATS_JOBS, BEE_NATS_DONE; empty means the
// default): every job goes to Jobs, every result to Done, the kind and the
// id travel inside the message (Envelope). The stream and consumer names
// derive from the subjects by one rule:
//
//	job subject       <jobs>                 bee.jobs
//	result subject    <done>                 bee.done
//	job stream        <jobs> as a name       BEE_JOBS   (dots → _, upper case)
//	result stream     <done> as a name       BEE_DONE
//	bee consumer      the job stream name    BEE_JOBS
//
// Bees with the same subjects share one queue; bees with other buckets on
// the same nats need other subjects.
type Names struct {
	Jobs string
	Done string
}

// JobsSubject is the subject of every job, bee.jobs by default
func (a Names) JobsSubject() string {
	if a.Jobs == "" {
		return "bee.jobs"
	}

	return a.Jobs
}

// DoneSubject is the subject of every result, bee.done by default
func (a Names) DoneSubject() string {
	if a.Done == "" {
		return "bee.done"
	}

	return a.Done
}

// Validate checks the subjects: tokens of A-Z, a-z, 0-9, _ and - joined by
// dots, no wildcards, not equal and not one inside the other
func (a Names) Validate() error {
	jobs := a.JobsSubject()
	done := a.DoneSubject()

	// each subject on its own
	for _, subject := range []string{jobs, done} {
		for _, token := range strings.Split(subject, ".") {
			if !a.token(token) {
				return fmt.Errorf("nats subject %q: tokens of A-Z, a-z, 0-9, _ and - joined by dots", subject)
			}
		}
	}

	// the two subjects apart
	switch {
	case jobs == done:
		return errors.New("nats subjects of jobs and results are equal: " + jobs)
	case strings.HasPrefix(jobs, done+"."):
		return fmt.Errorf("nats subject %q lies inside %q", jobs, done)
	case strings.HasPrefix(done, jobs+"."):
		return fmt.Errorf("nats subject %q lies inside %q", done, jobs)
	}

	return nil
}

// token reports whether s is one subject token of A-Z, a-z, 0-9, _ and -
func (a Names) token(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return false
		}
	}

	return true
}

// Path returns the http path of a job of type kind: POST /v1/<kind>
func (a Names) Path(kind Kind) string {
	return "/v1/" + string(kind)
}

// TrailerCode is the gRPC trailer with the error code of a failed job
func (a Names) TrailerCode() string {
	return "bee-error-code"
}

// TrailerRetryable is the gRPC trailer telling whether a retry is possible
func (a Names) TrailerRetryable() string {
	return "bee-retryable"
}

// JobsStream is the JetStream stream of the jobs (a work queue)
func (a Names) JobsStream() string {
	return a.name(a.JobsSubject())
}

// DoneStream is the JetStream stream of the results, kept
// BEE_NATS_RESULT_TTL
func (a Names) DoneStream() string {
	return a.name(a.DoneSubject())
}

// Consumer is the durable pull consumer every bee of the group shares
func (a Names) Consumer() string {
	return a.JobsStream()
}

// name is a subject as a JetStream name: dots to _, upper case
func (a Names) name(subject string) string {
	return strings.ToUpper(strings.ReplaceAll(subject, ".", "_"))
}

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
// the nats subject bases (BEE_NATS_JOBS, BEE_NATS_DONE; empty means the
// default). Everything else is derived from them by one rule:
//
//	job subject       <jobs>.<kind>          bee.jobs.new.audio
//	result subject    <done>.<kind>.<id>     bee.jobs.done.audio.track-1
//	job stream        <jobs> as a name       BEE_JOBS_NEW   (dots → _, upper case)
//	result stream     <done> as a name       BEE_JOBS_DONE
//	consumer          the job stream name    BEE_JOBS_NEW
//
// Bees with the same bases share one queue; bees with other buckets on
// the same nats need other bases.
type Names struct {
	Jobs string
	Done string
}

// JobsBase is the job subject base, bee.jobs.new by default
func (a Names) JobsBase() string {
	if a.Jobs == "" {
		return "bee.jobs.new"
	}

	return a.Jobs
}

// DoneBase is the result subject base, bee.jobs.done by default
func (a Names) DoneBase() string {
	if a.Done == "" {
		return "bee.jobs.done"
	}

	return a.Done
}

// Validate checks the bases: tokens of A-Z, a-z, 0-9, _ and - joined by
// dots, no wildcards, not equal and not one inside the other
func (a Names) Validate() error {
	jobs := a.JobsBase()
	done := a.DoneBase()

	// each base on its own
	for _, base := range []string{jobs, done} {
		for _, token := range strings.Split(base, ".") {
			if !a.token(token) {
				return fmt.Errorf("nats subject base %q: tokens of A-Z, a-z, 0-9, _ and - joined by dots", base)
			}
		}
	}

	// the two bases apart
	switch {
	case jobs == done:
		return errors.New("nats subject bases of jobs and results are equal: " + jobs)
	case strings.HasPrefix(jobs, done+"."):
		return fmt.Errorf("nats subject base %q lies inside %q", jobs, done)
	case strings.HasPrefix(done, jobs+"."):
		return fmt.Errorf("nats subject base %q lies inside %q", done, jobs)
	}

	return nil
}

// token reports whether s is one subject token of A-Z, a-z, 0-9, _ and -;
// the same rule as a job id
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

// JobsSubject is the subject of the jobs of kind: <jobs>.<kind>
func (a Names) JobsSubject(kind Kind) string {
	return a.JobsBase() + "." + string(kind)
}

// AllJobs are the subjects of the jobs of every kind: <jobs>.*
func (a Names) AllJobs() string {
	return a.JobsBase() + ".*"
}

// Kind is the kind of a job subject <jobs>.<kind>; not Valid when the
// subject is not one
func (a Names) Kind(subject string) Kind {
	kind, ok := strings.CutPrefix(subject, a.JobsBase()+".")
	if !ok {
		return ""
	}

	return Kind(kind)
}

// DoneSubject is the subject of the result of job id of kind:
// <done>.<kind>.<id>; the id is a valid subject token (see tools.ID)
func (a Names) DoneSubject(kind Kind, id string) string {
	return a.DoneBase() + "." + string(kind) + "." + id
}

// AllDone are the subjects of every result: <done>.>
func (a Names) AllDone() string {
	return a.DoneBase() + ".>"
}

// JobsStream is the JetStream stream of the jobs (a work queue)
func (a Names) JobsStream() string {
	return a.name(a.JobsBase())
}

// DoneStream is the JetStream stream of the results, kept
// BEE_NATS_RESULT_TTL
func (a Names) DoneStream() string {
	return a.name(a.DoneBase())
}

// Consumer is the durable pull consumer every bee of the group shares
func (a Names) Consumer() string {
	return a.JobsStream()
}

// name is a subject base as a JetStream name: dots to _, upper case
func (a Names) name(base string) string {
	return strings.ToUpper(strings.ReplaceAll(base, ".", "_"))
}

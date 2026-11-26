// Package proto is the bee protocol shared by servers and clients:
// bee.proto and the generated gRPC code, job types (Kind) and transport
// names (Names): http paths, gRPC trailers, nats stream and subjects.
//
// On every transport the job and the result are json of the public types
// github.com/angels-you/bee/pkg/types/audio, video, images
package proto

import (
	"crypto/sha256"
	"encoding/hex"
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

// Names are the transport names of one bee. Name is BEE_NATS_NAME (empty —
// "bee"): bees with different buckets on one nats have different names
type Names struct {
	Name string
}

// name returns the bee name, bee by default
func (a Names) name() string {
	if a.Name == "" {
		return "bee"
	}

	return a.Name
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

// Stream is the JetStream job stream (work queue): BEE
func (a Names) Stream() string {
	return strings.ToUpper(a.name())
}

// Results is the JetStream results stream: BEE_RESULTS; keeps results for
// BEE_NATS_RESULT_TTL, the client reads them
func (a Names) Results() string {
	return a.Stream() + "_RESULTS"
}

// Ledger is the JetStream key-value ledger of finished jobs: BEE_LEDGER
func (a Names) Ledger() string {
	return a.Stream() + "_LEDGER"
}

// Durable is the shared job pull consumer of all bees with this name
func (a Names) Durable() string {
	return a.name()
}

// Jobs is the job subject of type kind: bee.audio.new
func (a Names) Jobs(kind Kind) string {
	return a.name() + "." + string(kind) + ".new"
}

// AllJobs are the job subjects of all types: bee.*.new
func (a Names) AllJobs() string {
	return a.name() + ".*.new"
}

// Done is the result subject of job id of type kind:
// bee.audio.done.<sha256(id) hex, 32 chars>; the id cannot go into a
// subject as is (dots, spaces, *, >)
func (a Names) Done(kind Kind, id string) string {
	sum := sha256.Sum256([]byte(id))

	return a.name() + "." + string(kind) + ".done." + hex.EncodeToString(sum[:16])
}

// AllDone are the result subjects of all types and jobs: bee.*.done.>
func (a Names) AllDone() string {
	return a.name() + ".*.done.>"
}

// MsgID is the job Nats-Msg-Id: publishing the same job again within the
// JetStream deduplication window does not create a second one
func (a Names) MsgID(kind Kind, id string) string {
	return string(kind) + ":" + id
}

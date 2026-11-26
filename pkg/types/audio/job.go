// Package audio holds the audio bee job and its result, the same on any
// transport (http, grpc, nats). No server dependencies: the client imports
// this package
package audio

import (
	"path"
	"strings"

	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/code"
)

// Job is one track. Input is the url of the original (presigned GET or any
// public https), logged without the query.
//
// Result keys (Output, Wave, Metadata, Analysis) are chosen by the client: they
// are keys in the bee storage (not a bucket and not an endpoint). Output is
// required, the rest are not: a set key gets its result published there, an
// empty one is skipped. bee writes over whatever lies at the keys.
//
// Key is an optional AES-128 track key: with it every HLS segment is
// encrypted, without it HLS is published in the clear; it is never logged
// and never stored
type Job struct {
	ID       any            `json:"id"`                  // a string or a number, returned as sent
	UserMeta map[string]any `json:"user_meta,omitempty"` // data of the caller, returned untouched in the result
	Input    string         `json:"input"`               // url of the original

	// Output is the master playlist key (.m3u8); each rendition lives next
	// to it in a folder named after its bitrate in kbit/s:
	// <output folder>/<kbit>/index.m3u8, init.mp4, seg_NNNN.m4s
	Output string `json:"output"`

	Wave     string `json:"wave,omitempty"`     // waveform json key; empty — no waveform (and no Result.Waveform)
	Metadata string `json:"metadata,omitempty"` // metadata json key; empty — not published
	Analysis string `json:"analysis,omitempty"` // analysis json key; empty — not published

	Key    []byte `json:"key,omitempty"`     // empty (clear HLS) or exactly 16 bytes; base64 in json
	KeyURI string `json:"key_uri,omitempty"` // required with Key: key address for the player, e.g. skd://1001

	Options Options `json:"options,omitempty"`
}

// Options are the processing parameters. An empty field means the bee
// default (env BEE_AUDIO_*)
type Options struct {
	Normalize bool `json:"normalize,omitempty"` // normalize loudness to LUFS, not above TruePeak

	LUFS     float64 `json:"lufs,omitempty"`      // loudness target, below 0 and above -70 LUFS; 0 — BEE_AUDIO_LUFS
	TruePeak float64 `json:"true_peak,omitempty"` // true peak ceiling, below 0 dBTP; 0 — BEE_AUDIO_TRUE_PEAK

	// Renditions are the AAC bitrates of the HLS renditions, kbit/s,
	// distinct and positive, in master playlist order: the player starts
	// with the first. Empty — BEE_AUDIO_RENDITIONS
	Renditions []int `json:"renditions,omitempty"`

	SampleRate int `json:"sample_rate,omitempty"` // Hz of each rendition; 0 — BEE_AUDIO_SAMPLE_RATE
	Channels   int `json:"channels,omitempty"`    // channels of each rendition; 0 — BEE_AUDIO_CHANNELS
	Segment    int `json:"segment,omitempty"`     // HLS segment length, seconds; 0 — BEE_AUDIO_SEGMENT

	WaveformPPS    int `json:"waveform_pps,omitempty"`    // points per second in the waveform json; 0 — BEE_AUDIO_WAVEFORM_PPS
	WaveformPoints int `json:"waveform_points,omitempty"` // points in Result.Waveform; 0 — BEE_AUDIO_WAVEFORM_POINTS

	// flags: nil — bee default, false and true are meaningful values
	Analysis    *bool `json:"analysis,omitempty"`    // quality diagnostics (Result.Quality); nil — BEE_AUDIO_ANALYSIS
	Fingerprint *bool `json:"fingerprint,omitempty"` // chromaprint (Result.Fingerprint); nil — BEE_AUDIO_FINGERPRINT
}

// Validate checks the job regardless of bee settings: id, url, keys,
// encryption key and options. Result keys are cleaned in place. The url
// scheme and host, limits and rendition folders are checked by the engine
func (a *Job) Validate() *code.Error {
	if !tools.ID(a.ID) {
		return code.New(code.InvalidJob, "id must be 1..128 printable characters", nil)
	}

	// url of the original
	_, ok := tools.URL(a.Input)
	if !ok {
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	}

	// result keys
	e := a.validateKeys()
	if e != nil {
		return e
	}

	// encryption key
	e = a.validateKey()
	if e != nil {
		return e
	}

	return a.Options.Validate()
}

// validateKeys checks that Output is set and is .m3u8, the other keys are
// optional; all are clean and distinct
func (a *Job) validateKeys() *code.Error {
	taken := map[string]string{}

	// master playlist is required
	if a.Output == "" {
		return code.New(code.InvalidPath, "output is required", nil)
	}

	e := a.cleanKey("output", &a.Output, taken)
	if e != nil {
		return e
	}

	if path.Ext(a.Output) != ".m3u8" {
		return code.New(code.InvalidPath, "output must end in .m3u8", nil)
	}

	// waveform
	e = a.cleanKey("wave", &a.Wave, taken)
	if e != nil {
		return e
	}

	// metadata
	e = a.cleanKey("metadata", &a.Metadata, taken)
	if e != nil {
		return e
	}

	// analysis
	return a.cleanKey("analysis", &a.Analysis, taken)
}

// cleanKey checks that key name is clean and not taken by another; an empty
// one is skipped. taken holds the keys already taken and their names
func (a *Job) cleanKey(name string, raw *string, taken map[string]string) *code.Error {
	if *raw == "" {
		return nil
	}

	key, ok := tools.Key(*raw)
	switch {
	case !ok:
		return code.New(code.InvalidPath, name+" must be a clean relative storage key", nil)
	case taken[key] != "":
		return code.New(code.InvalidPath, name+" is the same key as "+taken[key], nil)
	}

	taken[key] = name
	*raw = key

	return nil
}

// validateKey checks the encryption key: empty or 16 bytes, KeyURI required
// with it
func (a *Job) validateKey() *code.Error {
	if len(a.Key) == 0 {
		return nil
	}

	switch {
	case len(a.Key) != 16:
		return code.New(code.InvalidJob, "key must be 16 bytes", nil)
	case a.KeyURI == "":
		return code.New(code.InvalidJob, "key_uri is required with a key", nil)
	case strings.ContainsAny(a.KeyURI, "\"\r\n"):
		return code.New(code.InvalidJob, "key_uri holds a quote or a line break", nil)
	}

	return nil
}

// Validate checks the options are within their limits; empty ones are not
// checked: they are bee defaults
func (a *Options) Validate() *code.Error {

	// loudness
	switch {
	case a.LUFS > 0:
		return code.New(code.InvalidJob, "lufs must be below 0", nil)
	case a.LUFS != 0 && a.LUFS <= -70:
		return code.New(code.InvalidJob, "lufs must be above -70", nil)
	case a.TruePeak > 0:
		return code.New(code.InvalidJob, "true_peak must be below 0", nil)
	}

	// renditions: the folder of a rendition is its bitrate, so distinct and positive
	seen := map[int]bool{}
	for _, kbit := range a.Renditions {
		switch {
		case kbit <= 0:
			return code.New(code.InvalidJob, "renditions must be positive kbit/s", nil)
		case seen[kbit]:
			return code.New(code.InvalidJob, "renditions must be distinct", nil)
		}
		seen[kbit] = true
	}

	// format and waveform
	switch {
	case a.SampleRate < 0:
		return code.New(code.InvalidJob, "sample_rate must not be negative", nil)
	case a.Channels < 0:
		return code.New(code.InvalidJob, "channels must not be negative", nil)
	case a.Segment < 0:
		return code.New(code.InvalidJob, "segment must not be negative", nil)
	case a.WaveformPPS < 0:
		return code.New(code.InvalidJob, "waveform_pps must not be negative", nil)
	case a.WaveformPoints < 0:
		return code.New(code.InvalidJob, "waveform_points must not be negative", nil)
	}

	return nil
}

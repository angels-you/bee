package audio

import "github.com/angels-you/bee/pkg/types/code"

// Result is the result of one track: what bee measured. Result keys are
// the client's keys (Job.Output, Wave, Meta, Analysis) and are not repeated
// here. A failed job has Error: none of its results count, whatever is
// already in storage
type Result struct {
	ID      string `json:"id"`
	Elapsed int64  `json:"elapsed"` // milliseconds for the whole job

	SHA256     string `json:"sha256,omitempty"`      // of the original
	SourceSize int64  `json:"source_size,omitempty"` // bytes of the original
	Size       int64  `json:"size,omitempty"`        // bytes published

	// Encrypted means the HLS segments are encrypted with Job.Key; false —
	// a job without a key, clear HLS
	Encrypted bool `json:"encrypted"`

	// Renditions are the published renditions in master playlist order
	Renditions []Rendition `json:"renditions,omitempty"`

	// Waveform holds the waveform json peaks, one byte per point (0 silence,
	// 255 full scale), Options.WaveformPoints of them; empty without
	// Job.Wave. Base64 in json
	Waveform []byte `json:"waveform,omitempty"`

	Metrics map[string]int64 `json:"metrics,omitempty"` // stage → milliseconds

	Meta        *Metadata    `json:"meta,omitempty"`
	Loudness    *Loudness    `json:"loudness,omitempty"`
	Quality     *Quality     `json:"quality,omitempty"`
	Fingerprint *Fingerprint `json:"fingerprint,omitempty"` // only in the response, never written to storage
	Warnings    []Warning    `json:"warnings,omitempty"`

	Error *code.Error `json:"error,omitempty"`
}

// Rendition is one published HLS bitrate
type Rendition struct {
	Folder           string `json:"folder"`            // next to master: <folder>/index.m3u8, init.mp4, seg_NNNN.m4s
	Bitrate          int    `json:"bitrate"`           // AAC target, bit/s
	AverageBandwidth int    `json:"average_bandwidth"` // over the published segments, bit/s
	PeakBandwidth    int    `json:"peak_bandwidth"`    // of the densest segment, bit/s
	Size             int64  `json:"size"`              // bytes published: media playlist, init and segments
}

// Warning is a non-critical quality note
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

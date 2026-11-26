package audio

import "github.com/angels-you/bee/pkg/types/code"

// Result — результат одного трека: то, что bee измерил. Ключи
// результатов — ключи клиента (Job.Output, Wave, Meta, Analysis), здесь
// они не повторяются. У упавшей job есть Error: ни один её результат не
// считается, что бы уже ни лежало в хранилище
type Result struct {
	ID      string `json:"id"`
	Elapsed int64  `json:"elapsed"` // миллисекунд на всю job

	SHA256     string `json:"sha256,omitempty"`      // оригинала
	SourceSize int64  `json:"source_size,omitempty"` // байт оригинала
	Size       int64  `json:"size,omitempty"`        // байт опубликовано

	// Encrypted — сегменты HLS зашифрованы Job.Key; false — job без ключа,
	// HLS открытый
	Encrypted bool `json:"encrypted"`

	// Renditions — опубликованные варианты в порядке master playlist
	Renditions []Rendition `json:"renditions,omitempty"`

	// Waveform — пики waveform json, по байту на точку (0 тишина, 255
	// полная шкала), Options.WaveformPoints штук; пусто без Job.Wave.
	// Base64 в json
	Waveform []byte `json:"waveform,omitempty"`

	Metrics map[string]int64 `json:"metrics,omitempty"` // этап → миллисекунд

	Meta        *Metadata    `json:"meta,omitempty"`
	Loudness    *Loudness    `json:"loudness,omitempty"`
	Quality     *Quality     `json:"quality,omitempty"`
	Fingerprint *Fingerprint `json:"fingerprint,omitempty"` // только в ответе, в хранилище не пишется
	Warnings    []Warning    `json:"warnings,omitempty"`

	Error *code.Error `json:"error,omitempty"`
}

// Rendition — один опубликованный битрейт HLS
type Rendition struct {
	Folder           string `json:"folder"`            // рядом с master: <folder>/index.m3u8, init.mp4, seg_NNNN.m4s
	Bitrate          int    `json:"bitrate"`           // цель AAC, bit/s
	AverageBandwidth int    `json:"average_bandwidth"` // по опубликованным сегментам, bit/s
	PeakBandwidth    int    `json:"peak_bandwidth"`    // самого плотного сегмента, bit/s
	Size             int64  `json:"size"`              // байт опубликовано: media playlist, init и сегменты
}

// Warning — некритичное замечание о качестве
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

package audio

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// Waveform — amplitude envelope for players. Every bucket covers
// 1/SampleRate seconds of the mono mix; Min and Max are the peak sample
// values of the bucket in [-1, 1], RMS is in [0, 1]. All three arrays have
// the same length. Peaks are never lost: each bucket keeps the extreme
// samples, downsampling merges buckets by min/max.
type Waveform struct {
	Version    int       `json:"version"`
	DurationMs int64     `json:"duration_ms"`
	SampleRate int       `json:"sample_rate"` // buckets per second
	Channels   int       `json:"channels"`    // 1: mono mix
	Min        []float32 `json:"min"`
	Max        []float32 `json:"max"`
	RMS        []float32 `json:"rms"`
}

// ErrNoPCM — empty input.
var ErrNoPCM = errors.New("audio: no pcm samples")

// BuildWaveform reads mono float32 little-endian PCM at sampleRate,
// applies gainDB and folds it into pointsPerSecond buckets. Values beyond
// full scale after gain are clamped.
func BuildWaveform(r io.Reader, sampleRate, pointsPerSecond int, gainDB float64) (w Waveform, err error) {
	if sampleRate <= 0 || pointsPerSecond <= 0 {
		return w, errors.New("audio: sample rate and points per second must be positive")
	}
	bucket := sampleRate / pointsPerSecond
	if bucket < 1 {
		bucket = 1
	}
	gain := float32(GainLinear(gainDB))

	w.Version = 1
	w.SampleRate = pointsPerSecond
	w.Channels = 1

	br := bufio.NewReaderSize(r, 256*1024)
	var (
		buf     [4]byte
		n       int
		total   int64
		lo, hi  float32 = 1, -1
		sumsq   float64
		flushed = func() {
			w.Min = append(w.Min, clamp(lo))
			w.Max = append(w.Max, clamp(hi))
			w.RMS = append(w.RMS, clamp(float32(math.Sqrt(sumsq/float64(n)))))
			lo, hi, sumsq, n = 1, -1, 0, 0
		}
	)
	for {
		if _, err = io.ReadFull(br, buf[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				err = nil
				break
			}
			return
		}
		v := math.Float32frombits(binary.LittleEndian.Uint32(buf[:])) * gain
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
		sumsq += float64(v) * float64(v)
		n++
		total++
		if n == bucket {
			flushed()
		}
	}
	if n > 0 {
		flushed()
	}
	if total == 0 {
		return w, ErrNoPCM
	}
	w.DurationMs = int64(math.Round(float64(total) * 1000 / float64(sampleRate)))
	return w, nil
}

func clamp(v float32) float32 {
	switch {
	case v > 1:
		return 1
	case v < -1:
		return -1
	case v != v: // NaN
		return 0
	}
	return v
}

// Downsample — a waveform with at most points buckets by merging
// neighbours (min of mins, max of maxs, rms of the squares). For
// thumbnails without touching the audio again.
func (w Waveform) Downsample(points int) Waveform {
	n := len(w.Min)
	if points <= 0 || points >= n {
		return w
	}
	res := Waveform{Version: w.Version, DurationMs: w.DurationMs, Channels: w.Channels}
	res.SampleRate = int(math.Round(float64(points) * 1000 / float64(max64(w.DurationMs, 1))))
	if res.SampleRate < 1 {
		res.SampleRate = 1
	}
	for i := 0; i < points; i++ {
		from, to := i*n/points, (i+1)*n/points
		if to <= from {
			to = from + 1
		}
		lo, hi := float32(1), float32(-1)
		var sumsq float64
		for j := from; j < to; j++ {
			lo = minf(lo, w.Min[j])
			hi = maxf(hi, w.Max[j])
			sumsq += float64(w.RMS[j]) * float64(w.RMS[j])
		}
		res.Min = append(res.Min, lo)
		res.Max = append(res.Max, hi)
		res.RMS = append(res.RMS, float32(math.Sqrt(sumsq/float64(to-from))))
	}
	return res
}

// Peaks — points bytes for Result.Waveform: point i is the
// largest absolute peak (max of -Min and Max) of buckets [i*n/512,
// (i+1)*n/512) of the n buckets, scaled so that full scale 1.0 is 255.
// Peaks are never lost: every bucket counts. With fewer buckets than
// points a point takes its nearest bucket, i*n/512, so buckets repeat.
// Silence gives zeros; no buckets give all zeros.
func (w Waveform) Peaks(points int) []byte {
	n := len(w.Min)
	out := make([]byte, points)
	if n == 0 {
		return out
	}
	for i := range out {
		from, to := i*n/points, (i+1)*n/points
		if to <= from {
			to = from + 1
		}
		var amp float32
		for j := from; j < to; j++ {
			amp = maxf(amp, maxf(-w.Min[j], w.Max[j]))
		}
		out[i] = byte(math.Round(float64(amp) * 255))
	}
	return out
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

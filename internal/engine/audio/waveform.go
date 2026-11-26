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

// waveBucket — the bucket BuildWaveform is filling.
type waveBucket struct {
	lo    float32
	hi    float32
	sumsq float64
	n     int
}

// reset — an empty bucket.
func (b *waveBucket) reset() {
	b.lo = 1
	b.hi = -1
	b.sumsq = 0
	b.n = 0
}

// add — one sample into the bucket.
func (b *waveBucket) add(v float32) {
	if v < b.lo {
		b.lo = v
	}
	if v > b.hi {
		b.hi = v
	}
	b.sumsq += float64(v) * float64(v)
	b.n++
}

// BuildWaveform reads mono float32 little-endian PCM at sampleRate,
// applies gainDB and folds it into pointsPerSecond buckets. Values beyond
// full scale after gain are clamped.
func (t Tools) BuildWaveform(r io.Reader, sampleRate, pointsPerSecond int, gainDB float64) (w Waveform, err error) {
	// rates must be positive
	switch {
	case sampleRate <= 0:
		return w, errors.New("audio: sample rate and points per second must be positive")
	case pointsPerSecond <= 0:
		return w, errors.New("audio: sample rate and points per second must be positive")
	}

	// samples per bucket and gain factor
	size := sampleRate / pointsPerSecond
	if size < 1 {
		size = 1
	}
	gain := float32(t.GainLinear(gainDB))

	w.Version = 1
	w.SampleRate = pointsPerSecond
	w.Channels = 1

	// read sample by sample, flush every full bucket
	br := bufio.NewReaderSize(r, 256*1024)
	var buf [4]byte
	var total int64
	var bucket waveBucket
	bucket.reset()

	for {
		_, err = io.ReadFull(br, buf[:])
		if err != nil {
			// end of input, a partial sample is dropped
			switch {
			case errors.Is(err, io.EOF):
				err = nil
			case errors.Is(err, io.ErrUnexpectedEOF):
				err = nil
			default:
				return
			}
			break
		}

		v := math.Float32frombits(binary.LittleEndian.Uint32(buf[:])) * gain
		bucket.add(v)
		total++

		if bucket.n == size {
			t.flush(&w, &bucket)
		}
	}

	// the last partial bucket
	if bucket.n > 0 {
		t.flush(&w, &bucket)
	}

	if total == 0 {
		return w, t.ErrNoPCM()
	}

	w.DurationMs = int64(math.Round(float64(total) * 1000 / float64(sampleRate)))
	return w, nil
}

// flush — appends the bucket to the waveform, clamped, and empties it.
func (t Tools) flush(w *Waveform, b *waveBucket) {
	rms := float32(math.Sqrt(b.sumsq / float64(b.n)))

	w.Min = append(w.Min, t.clamp(b.lo))
	w.Max = append(w.Max, t.clamp(b.hi))
	w.RMS = append(w.RMS, t.clamp(rms))

	b.reset()
}

// clamp — v within [-1, 1]; NaN → 0.
func (t Tools) clamp(v float32) float32 {
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
	switch {
	case points <= 0:
		return w
	case points >= n:
		return w
	}

	// buckets per second of the result
	res := Waveform{Version: w.Version, DurationMs: w.DurationMs, Channels: w.Channels}
	res.SampleRate = int(math.Round(float64(points) * 1000 / float64(max(w.DurationMs, 1))))
	if res.SampleRate < 1 {
		res.SampleRate = 1
	}

	// merge the buckets [from, to) of each point
	for i := 0; i < points; i++ {
		from := i * n / points
		to := (i + 1) * n / points
		if to <= from {
			to = from + 1
		}

		lo := float32(1)
		hi := float32(-1)
		var sumsq float64
		for j := from; j < to; j++ {
			lo = w.minf(lo, w.Min[j])
			hi = w.maxf(hi, w.Max[j])
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
		// buckets of the point, at least one
		from := i * n / points
		to := (i + 1) * n / points
		if to <= from {
			to = from + 1
		}

		// the largest absolute peak
		var amp float32
		for j := from; j < to; j++ {
			amp = w.maxf(amp, w.maxf(-w.Min[j], w.Max[j]))
		}

		out[i] = byte(math.Round(float64(amp) * 255))
	}

	return out
}

// minf — the smaller of a and b; b when they do not compare.
func (w Waveform) minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// maxf — the larger of a and b; b when they do not compare.
func (w Waveform) maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

package audio

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	types "github.com/angels-you/bee/pkg/types/audio"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Floors of the measured values. ffmpeg prints -inf for the level of
// digital silence and JSON has no infinity, so a non-finite value (-inf,
// nan, unparsable) is replaced by its floor where it is parsed: every
// Loudness value and every dB level of Quality is finite.

// LevelFloor — dB floor of peaks, RMS and the flat factor: -144 dB is the
// dynamic range of 24-bit PCM (6.02 dB × 24), below the quietest non-zero
// sample of any delivered source. A sample peak at the floor means digital
// silence.
func (t Tools) LevelFloor() float64 {
	return -144.0
}

// LoudnessFloor — LUFS floor of integrated loudness and gate thresholds:
// -70 LUFS is the absolute gate of ITU-R BS.1770, the lowest integrated
// loudness the measurement can report (ffmpeg itself prints -70.0 for
// silence).
func (t Tools) LoudnessFloor() float64 {
	return -70.0
}

// Loudness is EBU R128: public job type
type Loudness = types.Loudness

// Silence is a span of silence: public job type
type Silence = types.Silence

// Quality is the quality diagnostics: public job type
type Quality = types.Quality

// AnalyzeOptions — what to measure in one decode pass.
type AnalyzeOptions struct {
	// Quality adds astats and silencedetect to the pass.
	Quality bool
	// PCMPath — write mono float32 PCM (native sample rate) for the
	// waveform; empty = skip.
	PCMPath string

	SilenceDB  float64 // threshold, with Quality
	SilenceMin float64 // seconds, with Quality
	// DurationSec of the input: trailing silence without an end marker
	// runs to this point.
	DurationSec float64
}

// Analysis — result of Analyze.
type Analysis struct {
	Loudness Loudness
	Quality  Quality
}

// Analyze decodes the file once and measures loudness (ebur128), quality
// (astats, silencedetect) and optionally dumps the PCM for the waveform.
// The input can be anything ffmpeg opens, including an HLS playlist.
func (t Tools) Analyze(ctx context.Context, file string, o AnalyzeOptions) (res Analysis, err error) {
	// one split output per consumer: loudness, quality (stats, silence), pcm
	labels := []string{"[l]"}
	if o.Quality {
		labels = append(labels, "[s]", "[d]")
	}
	if o.PCMPath != "" {
		labels = append(labels, "[p]")
	}

	// filter graph: one decode, then loudness
	graph := "[0:a:0]asplit=" + strconv.Itoa(len(labels)) + strings.Join(labels, "") + ";" +
		"[l]ebur128=peak=true+sample:framelog=verbose[lo]"
	args := []string{"-nostdin", "-hide_banner", "-nostats", "-loglevel", "info", "-y", "-i", file}
	maps := []string{"-map", "[lo]", "-f", "null", "-"}

	// quality: stats and silence
	if o.Quality {
		stats := ";[s]astats=measure_perchannel=Peak_level+RMS_level+Flat_factor+Peak_count+DC_offset:measure_overall=Peak_level+RMS_level+Flat_factor+Peak_count+DC_offset[so]"
		silence := fmt.Sprintf(";[d]silencedetect=n=%.1fdB:d=%.2f[do]", o.SilenceDB, o.SilenceMin)
		graph += stats + silence
		maps = append(maps, "-map", "[so]", "-f", "null", "-", "-map", "[do]", "-f", "null", "-")
	}

	// pcm for the waveform
	if o.PCMPath != "" {
		graph += ";[p]aformat=sample_fmts=flt:channel_layouts=mono[po]"
		maps = append(maps, "-map", "[po]", "-c:a", "pcm_f32le", "-f", "f32le", o.PCMPath)
	}

	args = append(args, "-filter_complex", graph)
	args = append(args, maps...)

	// run
	_, stderr, err := t.run(ctx, "", t.FFmpeg, args...)
	if err != nil {
		if ctx.Err() != nil {
			return res, err
		}
		return res, fmt.Errorf("%w: %v", t.ErrInvalid(), err)
	}

	// loudness
	res.Loudness, err = t.parseEBUR128(stderr)
	if err != nil {
		return res, err
	}

	// quality
	if o.Quality {
		var rms float64
		res.Quality, rms = t.parseQuality(stderr, o.DurationSec)
		res.Loudness.RMS = rms
		res.Quality.DynamicRange = t.round2(res.Loudness.SamplePeak - res.Loudness.RMS)
	}

	return res, nil
}

// parseEBUR128 — the "Summary:" block of the ebur128 filter.
func (t Tools) parseEBUR128(stderr []byte) (l Loudness, err error) {
	section := ""
	found := false

	sc := bufio.NewScanner(bytes.NewReader(stderr))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for sc.Scan() {
		line := sc.Text()

		// ebur128 lines lose their prefix, lines of other filters are skipped
		i := strings.Index(line, "] ")
		switch {
		case i >= 0 && strings.HasPrefix(line, "[Parsed_ebur128"):
			line = line[i+2:]
		case strings.HasPrefix(line, "["):
			continue
		}
		line = strings.TrimSpace(line)

		// values of the summary by section
		switch {
		case line == "Summary:":
			found = true
		case !found:
			continue
		case strings.HasSuffix(line, ":"):
			section = strings.TrimSuffix(line, ":")
		case strings.HasPrefix(line, "I:"):
			l.Integrated = t.dbValue(line, t.LoudnessFloor())
		case strings.HasPrefix(line, "Threshold:") && section == "Integrated loudness":
			l.Threshold = t.dbValue(line, t.LoudnessFloor())
		case strings.HasPrefix(line, "LRA:"):
			l.LRA = t.dbValue(line, 0) // a range is never negative
		case strings.HasPrefix(line, "Peak:") && section == "Sample peak":
			l.SamplePeak = t.dbValue(line, t.LevelFloor())
		case strings.HasPrefix(line, "Peak:") && section == "True peak":
			l.TruePeak = t.dbValue(line, t.LevelFloor())
		}
	}

	if !found {
		return l, fmt.Errorf("%w: no ebur128 summary", t.ErrInvalid())
	}
	return l, nil
}

// dbValue — "I:   -23.0 LUFS" → -23.0; "-inf", nan or no number → floor
func (t Tools) dbValue(line string, floor float64) float64 {
	f := strings.Fields(line)
	if len(f) < 2 {
		return floor
	}

	// the number before the unit, or the only value
	v := f[len(f)-2]
	if len(f) == 2 {
		v = f[1]
	}

	// only a finite number counts
	x, err := strconv.ParseFloat(v, 64)
	switch {
	case err != nil:
		return floor
	case math.IsInf(x, 0):
		return floor
	case math.IsNaN(x):
		return floor
	}
	return x
}

// quality parser state
type qualityParser struct {
	Quality
	rmsOverall float64
	channel    int // 0 = none yet, -1 = overall
	peakLevel  float64
	silStart   float64
	inSilence  bool
}

// parseQuality — astats per channel/overall and silencedetect lines.
func (t Tools) parseQuality(stderr []byte, duration float64) (Quality, float64) {
	q := qualityParser{}
	chRMS := map[int]float64{}

	sc := bufio.NewScanner(bytes.NewReader(stderr))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for sc.Scan() {
		raw := sc.Text()

		// only "[tag] line" lines
		i := strings.Index(raw, "] ")
		switch {
		case i < 0:
			continue
		case !strings.HasPrefix(raw, "["):
			continue
		}
		tag := raw[1:i]
		line := strings.TrimSpace(raw[i+2:])

		switch {
		case strings.HasPrefix(tag, "Parsed_astats"):
			t.parseStats(&q, chRMS, line)
		case strings.Contains(tag, "silencedetect"):
			t.parseSilence(&q, line)
		}
	}

	// silence running to the end of the file has no silence_end line
	if q.inSilence && duration > q.silStart {
		q.Silences = append(q.Silences, Silence{Start: q.silStart, End: duration, Duration: duration - q.silStart})
	}

	// leading and trailing silence
	for _, s := range q.Silences {
		if s.Start <= 0.05 {
			q.LeadingSilence = t.round2(s.Duration)
		}
		if duration > 0 && s.End >= duration-0.05 {
			q.TrailingSilence = t.round2(s.Duration)
		}
	}

	// clipping: the peak is within 0.1 dB of full scale and the signal has
	// runs of equal samples at its extreme levels (isolated extremes give a
	// flat factor of 0 dB, runs make it positive)
	q.Clipping = q.peakLevel >= -0.1 && q.FlatFactor > 0

	// channel rms in channel order and the imbalance
	if len(chRMS) > 0 {
		keys := make([]int, 0, len(chRMS))
		for k := range chRMS {
			keys = append(keys, k)
		}
		sort.Ints(keys)

		// a silent channel is at LevelFloor: the largest imbalance there is
		lo := math.Inf(1)
		hi := math.Inf(-1)
		for _, k := range keys {
			v := chRMS[k]
			q.ChannelRMS = append(q.ChannelRMS, t.round2(v))
			lo = math.Min(lo, v)
			hi = math.Max(hi, v)
		}
		q.ChannelImbalance = t.round2(hi - lo)
	}

	return q.Quality, q.rmsOverall
}

// parseStats — one astats line: "Channel: n", "Overall" or "key: value".
func (t Tools) parseStats(q *qualityParser, chRMS map[int]float64, line string) {
	key, val, _ := strings.Cut(line, ":")
	key = strings.TrimSpace(key)
	val = strings.TrimSpace(val)

	switch key {
	case "Channel":
		q.channel, _ = strconv.Atoi(val)
	case "Overall":
		q.channel = -1
	case "RMS level dB":
		switch {
		case q.channel > 0:
			chRMS[q.channel] = t.dbValue("x: "+val, t.LevelFloor())
		case q.channel == -1:
			q.rmsOverall = t.dbValue("x: "+val, t.LevelFloor())
		}
	case "Peak level dB":
		if q.channel == -1 {
			q.peakLevel = t.dbValue("x: "+val, t.LevelFloor())
		}
	case "Flat factor":
		if q.channel == -1 {
			q.FlatFactor = t.dbValue("x: "+val, t.LevelFloor())
		}
	case "DC offset":
		if q.channel == -1 {
			q.DCOffset, _ = strconv.ParseFloat(val, 64)
		}
	}
}

// parseSilence — one silencedetect line: start or end of a silence.
func (t Tools) parseSilence(q *qualityParser, line string) {
	switch {
	case strings.HasPrefix(line, "silence_start:"):
		q.silStart = t.atof(strings.TrimPrefix(line, "silence_start:"))
		q.inSilence = true

	case strings.HasPrefix(line, "silence_end:"):
		end, rest, _ := strings.Cut(strings.TrimPrefix(line, "silence_end:"), "|")
		s := Silence{Start: q.silStart, End: t.atof(end)}

		// duration as reported, else from the bounds
		d := strings.TrimSpace(rest)
		if strings.HasPrefix(d, "silence_duration:") {
			s.Duration = t.atof(strings.TrimPrefix(d, "silence_duration:"))
		} else {
			s.Duration = s.End - s.Start
		}

		q.Silences = append(q.Silences, s)
		q.inSilence = false
	}
}

// round2 — v rounded to two decimals.
func (t Tools) round2(v float64) float64 {
	return math.Round(v*100) / 100
}

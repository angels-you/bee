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
const (
	// LevelFloor — dB floor of peaks, RMS and the flat factor: -144 dB is
	// the dynamic range of 24-bit PCM (6.02 dB × 24), below the quietest
	// non-zero sample of any delivered source. A sample peak at the floor
	// means digital silence.
	LevelFloor = -144.0
	// LoudnessFloor — LUFS floor of integrated loudness and gate
	// thresholds: -70 LUFS is the absolute gate of ITU-R BS.1770, the
	// lowest integrated loudness the measurement can report (ffmpeg itself
	// prints -70.0 for silence).
	LoudnessFloor = -70.0
)

// Loudness — EBU R128: публичный тип job
type Loudness = types.Loudness

// Silence — отрезок тишины: публичный тип job
type Silence = types.Silence

// Quality — диагностика качества: публичный тип job
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
	// filter graph: one decode, one split per consumer
	outputs := 1
	if o.Quality {
		outputs += 2
	}
	if o.PCMPath != "" {
		outputs++
	}
	labels := []string{"[l]"}
	if o.Quality {
		labels = append(labels, "[s]", "[d]")
	}
	if o.PCMPath != "" {
		labels = append(labels, "[p]")
	}
	graph := "[0:a:0]asplit=" + strconv.Itoa(outputs) + strings.Join(labels, "") + ";" +
		"[l]ebur128=peak=true+sample:framelog=verbose[lo]"
	args := []string{"-nostdin", "-hide_banner", "-nostats", "-loglevel", "info", "-y", "-i", file}
	maps := []string{"-map", "[lo]", "-f", "null", "-"}
	i := 1
	if o.Quality {
		graph += fmt.Sprintf(";[s]astats=measure_perchannel=Peak_level+RMS_level+Flat_factor+Peak_count+DC_offset:measure_overall=Peak_level+RMS_level+Flat_factor+Peak_count+DC_offset[so]"+
			";[d]silencedetect=n=%.1fdB:d=%.2f[do]", o.SilenceDB, o.SilenceMin)
		maps = append(maps, "-map", "[so]", "-f", "null", "-", "-map", "[do]", "-f", "null", "-")
		i += 2
	}
	if o.PCMPath != "" {
		graph += ";[p]aformat=sample_fmts=flt:channel_layouts=mono[po]"
		maps = append(maps, "-map", "[po]", "-c:a", "pcm_f32le", "-f", "f32le", o.PCMPath)
	}
	args = append(args, "-filter_complex", graph)
	args = append(args, maps...)

	_, stderr, err := t.run(ctx, "", t.FFmpeg, args...)
	if err != nil {
		if ctx.Err() != nil {
			return res, err
		}
		return res, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	res.Loudness, err = parseEBUR128(stderr)
	if err != nil {
		return res, err
	}
	if o.Quality {
		var rms float64
		res.Quality, rms = parseQuality(stderr, o.DurationSec)
		res.Loudness.RMS = rms
		res.Quality.DynamicRange = round2(res.Loudness.SamplePeak - res.Loudness.RMS)
	}
	return res, nil
}

// parseEBUR128 — the "Summary:" block of the ebur128 filter.
func parseEBUR128(stderr []byte) (l Loudness, err error) {
	section := ""
	found := false
	sc := bufio.NewScanner(bytes.NewReader(stderr))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "] "); i >= 0 && strings.HasPrefix(line, "[Parsed_ebur128") {
			line = line[i+2:]
		} else if strings.HasPrefix(line, "[") {
			continue
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "Summary:":
			found = true
		case !found:
			continue
		case strings.HasSuffix(line, ":"):
			section = strings.TrimSuffix(line, ":")
		case strings.HasPrefix(line, "I:"):
			l.Integrated = dbValue(line, LoudnessFloor)
		case strings.HasPrefix(line, "Threshold:") && section == "Integrated loudness":
			l.Threshold = dbValue(line, LoudnessFloor)
		case strings.HasPrefix(line, "LRA:"):
			l.LRA = dbValue(line, 0) // a range is never negative
		case strings.HasPrefix(line, "Peak:") && section == "Sample peak":
			l.SamplePeak = dbValue(line, LevelFloor)
		case strings.HasPrefix(line, "Peak:") && section == "True peak":
			l.TruePeak = dbValue(line, LevelFloor)
		}
	}
	if !found {
		return l, fmt.Errorf("%w: no ebur128 summary", ErrInvalid)
	}
	return l, nil
}

// dbValue — "I:   -23.0 LUFS" → -23.0; "-inf", nan or no number → floor
func dbValue(line string, floor float64) float64 {
	f := strings.Fields(line)
	if len(f) < 2 {
		return floor
	}
	v := f[len(f)-2]
	if len(f) == 2 {
		v = f[1]
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsInf(x, 0) || math.IsNaN(x) {
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
func parseQuality(stderr []byte, duration float64) (Quality, float64) {
	q := qualityParser{}
	chRMS := map[int]float64{}
	sc := bufio.NewScanner(bytes.NewReader(stderr))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		raw := sc.Text()
		i := strings.Index(raw, "] ")
		if i < 0 || !strings.HasPrefix(raw, "[") {
			continue
		}
		tag, line := raw[1:i], strings.TrimSpace(raw[i+2:])
		switch {
		case strings.HasPrefix(tag, "Parsed_astats"):
			key, val, _ := strings.Cut(line, ":")
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			switch key {
			case "Channel":
				q.channel, _ = strconv.Atoi(val)
			case "Overall":
				q.channel = -1
			case "RMS level dB":
				if q.channel > 0 {
					chRMS[q.channel] = dbValue("x: "+val, LevelFloor)
				} else if q.channel == -1 {
					q.rmsOverall = dbValue("x: "+val, LevelFloor)
				}
			case "Peak level dB":
				if q.channel == -1 {
					q.peakLevel = dbValue("x: "+val, LevelFloor)
				}
			case "Flat factor":
				if q.channel == -1 {
					q.FlatFactor = dbValue("x: "+val, LevelFloor)
				}
			case "DC offset":
				if q.channel == -1 {
					q.DCOffset, _ = strconv.ParseFloat(val, 64)
				}
			}
		case strings.Contains(tag, "silencedetect"):
			if strings.HasPrefix(line, "silence_start:") {
				q.silStart = atof(strings.TrimPrefix(line, "silence_start:"))
				q.inSilence = true
			} else if strings.HasPrefix(line, "silence_end:") {
				end, rest, _ := strings.Cut(strings.TrimPrefix(line, "silence_end:"), "|")
				s := Silence{Start: q.silStart, End: atof(end)}
				if d := strings.TrimSpace(rest); strings.HasPrefix(d, "silence_duration:") {
					s.Duration = atof(strings.TrimPrefix(d, "silence_duration:"))
				} else {
					s.Duration = s.End - s.Start
				}
				q.Silences = append(q.Silences, s)
				q.inSilence = false
			}
		}
	}
	// silence running to the end of the file has no silence_end line
	if q.inSilence && duration > q.silStart {
		q.Silences = append(q.Silences, Silence{Start: q.silStart, End: duration, Duration: duration - q.silStart})
	}
	for _, s := range q.Silences {
		if s.Start <= 0.05 {
			q.LeadingSilence = round2(s.Duration)
		}
		if duration > 0 && s.End >= duration-0.05 {
			q.TrailingSilence = round2(s.Duration)
		}
	}

	// clipping: the peak is within 0.1 dB of full scale and the signal has
	// runs of equal samples at its extreme levels (isolated extremes give a
	// flat factor of 0 dB, runs make it positive)
	q.Clipping = q.peakLevel >= -0.1 && q.FlatFactor > 0

	if len(chRMS) > 0 {
		keys := make([]int, 0, len(chRMS))
		for k := range chRMS {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		// a silent channel is at LevelFloor: the largest imbalance there is
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, k := range keys {
			v := chRMS[k]
			q.ChannelRMS = append(q.ChannelRMS, round2(v))
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		q.ChannelImbalance = round2(hi - lo)
	}
	return q.Quality, q.rmsOverall
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

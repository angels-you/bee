package video

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
)

// Loudness — the EBU R128 measure of an audio stream (the ebur128 filter
// with true peak). Every value is finite: where ffmpeg prints -inf (or no
// number) the floor is set: -70 LUFS for the integrated loudness (the
// absolute gate of BS.1770), -144 dB for the true peak (24-bit PCM), 0
// for the range.
type Loudness struct {
	Integrated float64 // LUFS
	TruePeak   float64 // dBTP
	LRA        float64 // LU
}

// Normalize — the linear normalization of a plan with Options.Normalize
// and sound: one audio-only ffmpeg pass over the same inputs and the same
// mix as the encode (inputs, mixChains) measures the integrated loudness
// and the true peak; Plan.Gain brings the loudness to Plan.LUFS without
// the true peak passing Plan.TruePeak (Loudness.gain), and the encode
// applies it as one volume filter before the fades. No compression, no
// limiter: the dynamics stay. Without Normalize or sound nothing runs.
func (a *Tools) Normalize(ctx context.Context, p *Plan) error {
	// nothing to normalize
	switch {
	case !p.Options.Normalize:
		return nil
	case !p.HasSound:
		return nil
	}

	// the inputs of the encode, without the watermark picture
	inputs, err := a.inputs(p, false)
	if err != nil {
		return err
	}

	// the loudness of the mix
	l, err := a.measure(ctx, inputs, p.mixChains(), "[mix]")
	if err != nil {
		return err
	}

	// the gain the encode applies
	p.Loudness = &l
	p.Gain = l.gain(p.LUFS, p.TruePeak)
	return nil
}

// measure — the loudness of the audio a graph ends in at label: ffmpeg
// runs the chains, the label through ebur128 into the null muxer, and the
// summary it logs is read.
func (a *Tools) measure(ctx context.Context, inputs, chains []string, label string) (Loudness, error) {
	// the chains, the label through ebur128
	graph := strings.Join(append(chains, label+"ebur128=peak=true:framelog=quiet[measured]"), ";")

	// the run into the null muxer; ebur128 logs at info
	args := []string{"-nostdin", "-hide_banner", "-nostats", "-loglevel", "info", "-y"}
	args = append(args, inputs...)
	args = append(args, "-filter_complex", graph, "-map", "[measured]", "-f", "null", "-")
	_, stderr, err := a.run(ctx, "", a.FFmpeg, args...)
	if err != nil {
		return Loudness{}, err
	}

	// the summary it logged
	l := Loudness{}
	err = l.parse(stderr)
	return l, err
}

// parse — the "Summary:" block ebur128 logs: I under "Integrated
// loudness", LRA under "Loudness range", Peak under "True peak".
func (a *Loudness) parse(log []byte) error {
	// the section of the summary the line is in, and the summary seen
	section := ""
	found := false

	// line by line
	sc := bufio.NewScanner(bytes.NewReader(log))
	for sc.Scan() {
		line := sc.Text()

		// the first line of the summary carries the filter prefix
		_, rest, prefixed := strings.Cut(line, "] ")
		if prefixed && strings.HasPrefix(line, "[Parsed_ebur128") {
			line = rest
		}
		line = strings.TrimSpace(line)

		// the values of the summary
		switch {
		case line == "Summary:":
			found = true
		case !found:
			// the frame log before the summary
		case strings.HasSuffix(line, ":"):
			section = strings.TrimSuffix(line, ":")
		case strings.HasPrefix(line, "I:"):
			a.Integrated = a.value(line, -70)
		case strings.HasPrefix(line, "LRA:"):
			a.LRA = a.value(line, 0)
		case strings.HasPrefix(line, "Peak:") && section == "True peak":
			a.TruePeak = a.value(line, -144)
		}
	}

	// no summary: the filter did not run to the end
	if !found {
		return errors.New("video: ebur128 logged no summary")
	}
	return sc.Err()
}

// value — the number of a summary line ("I:   -23.0 LUFS" → -23.0); floor
// for -inf, nan or no number.
func (a *Loudness) value(line string, floor float64) float64 {
	// the label, then the number
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return floor
	}

	// a finite number, or the floor
	x, err := strconv.ParseFloat(fields[1], 64)
	switch {
	case err != nil, math.IsInf(x, 0), math.IsNaN(x):
		return floor
	}
	return x
}

// gain — the linear gain in dB that brings the integrated loudness to
// target without the true peak passing ceiling: target − integrated,
// reduced to ceiling − true peak when that is lower (the rule of Bee's
// audio.ComputeGain), rounded to 0.01 dB. A mix with nothing measurable
// (the integrated loudness at its floor: silence) gets 0.
func (a Loudness) gain(target, ceiling float64) float64 {
	// silence: nothing to bring up
	if a.Integrated <= -70 {
		return 0
	}

	// to the target, but the true peak stays under the ceiling
	g := target - a.Integrated
	if a.TruePeak+g > ceiling {
		g = ceiling - a.TruePeak
	}

	return math.Round(g*100) / 100
}

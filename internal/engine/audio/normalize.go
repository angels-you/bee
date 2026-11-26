package audio

import "math"

// ComputeGain — linear gain in dB that brings the integrated loudness to
// target without the true peak exceeding maxTruePeak. When the target
// cannot be reached, the gain is reduced (limited = true) and dynamics are
// preserved: no compression, no limiter.
//
// Silence or a nearly silent input (below -60 LUFS, LoudnessFloor
// included) gets gain 0. The values of l are finite, as Analyze measures
// them.
func ComputeGain(l Loudness, targetLUFS, maxTruePeak float64) (gain float64, limited bool) {
	if l.Integrated < -60 {
		return 0, false
	}
	gain = targetLUFS - l.Integrated
	if l.TruePeak+gain > maxTruePeak {
		gain = maxTruePeak - l.TruePeak
		limited = true
	}
	return math.Round(gain*100) / 100, limited
}

// GainLinear — dB → amplitude factor.
func GainLinear(db float64) float64 {
	return math.Pow(10, db/20)
}

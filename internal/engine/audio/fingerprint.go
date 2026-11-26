package audio

import (
	"context"
	"encoding/json"
	"fmt"
	types "github.com/angels-you/bee/pkg/types/audio"
	"strconv"
	"strings"
)

// Fingerprint is chromaprint: public job type
type Fingerprint = types.Fingerprint

// Fingerprint runs fpcalc on the first maxSeconds (0 = fpcalc default 120).
func (t Tools) Fingerprint(ctx context.Context, file string, maxSeconds int) (fp Fingerprint, err error) {
	if t.Fpcalc == "" {
		return fp, ErrNoFpcalc
	}
	args := []string{"-json"}
	if maxSeconds > 0 {
		args = append(args, "-length", strconv.Itoa(maxSeconds))
	}
	args = append(args, file)
	out, _, err := t.run(ctx, "", t.Fpcalc, args...)
	if err != nil {
		return
	}
	var raw struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err = json.Unmarshal(out, &raw); err != nil {
		return fp, fmt.Errorf("audio: fpcalc output: %w", err)
	}
	if raw.Fingerprint == "" {
		return fp, fmt.Errorf("audio: fpcalc returned no fingerprint")
	}
	fp = Fingerprint{Algorithm: "chromaprint", Fingerprint: raw.Fingerprint, Duration: raw.Duration, MaxSeconds: maxSeconds}
	if v, _, e := t.run(ctx, "", t.Fpcalc, "-version"); e == nil {
		fp.Version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(v)), "fpcalc version"))
	}
	return fp, nil
}

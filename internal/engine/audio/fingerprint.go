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
	// fpcalc is optional
	if t.Fpcalc == "" {
		return fp, t.ErrNoFpcalc()
	}

	// arguments
	args := []string{"-json"}
	if maxSeconds > 0 {
		args = append(args, "-length", strconv.Itoa(maxSeconds))
	}
	args = append(args, file)

	// run
	out, _, err := t.run(ctx, "", t.Fpcalc, args...)
	if err != nil {
		return
	}

	// parse the json output
	var raw struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	err = json.Unmarshal(out, &raw)
	if err != nil {
		return fp, fmt.Errorf("audio: fpcalc output: %w", err)
	}

	if raw.Fingerprint == "" {
		return fp, fmt.Errorf("audio: fpcalc returned no fingerprint")
	}

	fp = Fingerprint{
		Algorithm:   "chromaprint",
		Fingerprint: raw.Fingerprint,
		Duration:    raw.Duration,
		MaxSeconds:  maxSeconds,
	}

	// fpcalc version, best effort
	version, _, e := t.run(ctx, "", t.Fpcalc, "-version")
	if e == nil {
		v := strings.TrimSpace(string(version))
		v = strings.TrimPrefix(v, "fpcalc version")
		fp.Version = strings.TrimSpace(v)
	}

	return fp, nil
}

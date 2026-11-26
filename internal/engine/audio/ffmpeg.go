// Package audio wraps ffmpeg/ffprobe/fpcalc for bee: probing, loudness and
// quality analysis, gain, AAC/HLS encoding, waveform rendering. Every
// command runs through exec without a shell, with the caller's context.
package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Tools — binaries: a path or a name looked up in PATH. FFmpeg and FFprobe
// are required; Fpcalc is optional, empty = no fingerprints.
type Tools struct {
	FFmpeg  string
	FFprobe string
	Fpcalc  string
}

// Errors of the audio package.
var (
	ErrNoAudio  = errors.New("audio: no audio stream")
	ErrInvalid  = errors.New("audio: cannot decode")
	ErrNoFpcalc = errors.New("audio: fpcalc is not configured")
)

// CommandError — a tool exited with an error; Stderr holds the tail of
// its output (local paths only, never urls).
type CommandError struct {
	Bin    string
	Args   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("%s: %v: %s", e.Bin, e.Err, e.Stderr)
}

func (e *CommandError) Unwrap() error { return e.Err }

// Check — ffmpeg and ffprobe are runnable; fpcalc only when set.
func (t Tools) Check() error {
	for _, bin := range []string{t.FFmpeg, t.FFprobe} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("audio: %s: %w", bin, err)
		}
	}
	if t.Fpcalc != "" {
		if _, err := exec.LookPath(t.Fpcalc); err != nil {
			return fmt.Errorf("audio: %s: %w", t.Fpcalc, err)
		}
	}
	return nil
}

// run executes bin with args in dir (empty = current) and returns stdout
// and stderr. A finished context is reported as its error, not as a
// command failure.
func (t Tools) run(ctx context.Context, dir, bin string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	stdout, stderr = out.Bytes(), errb.Bytes()
	if err != nil {
		if ctx.Err() != nil {
			return stdout, stderr, ctx.Err()
		}
		return stdout, stderr, &CommandError{Bin: bin, Args: args, Stderr: tail(errb.String(), 4096), Err: err}
	}
	return
}

// tail — last n bytes of s, trimmed.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

// Package audio wraps ffmpeg/ffprobe/fpcalc for bee: probing, loudness and
// quality analysis, gain, AAC/HLS encoding, waveform rendering. Every
// command runs through exec without a shell, with the caller's context.
package audio

import (
	"bytes"
	"context"
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

// Error — an error of the audio package. Values with the same text are
// equal, so errors.Is(err, tools.ErrInvalid()) works.
type Error string

// Error — the text of the error.
func (e Error) Error() string {
	return string(e)
}

// ErrNoAudio — the file has no audio stream.
func (t Tools) ErrNoAudio() error {
	return Error("audio: no audio stream")
}

// ErrInvalid — the file cannot be decoded.
func (t Tools) ErrInvalid() error {
	return Error("audio: cannot decode")
}

// ErrNoFpcalc — fingerprint asked without fpcalc.
func (t Tools) ErrNoFpcalc() error {
	return Error("audio: fpcalc is not configured")
}

// ErrPlaylist — a playlist does not meet the requirements.
func (t Tools) ErrPlaylist() error {
	return Error("audio: invalid playlist")
}

// ErrNoPCM — empty pcm input of the waveform.
func (t Tools) ErrNoPCM() error {
	return Error("audio: no pcm samples")
}

// CommandError — a tool exited with an error; Stderr holds the tail of
// its output (local paths only, never urls).
type CommandError struct {
	Bin    string
	Args   []string
	Stderr string
	Err    error
}

// Error — binary, exit error and the tail of stderr.
func (e *CommandError) Error() string {
	return fmt.Sprintf("%s: %v: %s", e.Bin, e.Err, e.Stderr)
}

// Unwrap — the exit error.
func (e *CommandError) Unwrap() error {
	return e.Err
}

// Check — ffmpeg and ffprobe are runnable; fpcalc only when set.
func (t Tools) Check() error {
	// required binaries
	for _, bin := range []string{t.FFmpeg, t.FFprobe} {
		_, err := exec.LookPath(bin)
		if err != nil {
			return fmt.Errorf("audio: %s: %w", bin, err)
		}
	}

	// optional fpcalc
	if t.Fpcalc != "" {
		_, err := exec.LookPath(t.Fpcalc)
		if err != nil {
			return fmt.Errorf("audio: %s: %w", t.Fpcalc, err)
		}
	}

	return nil
}

// run executes bin with args in dir (empty = current) and returns stdout
// and stderr. A finished context is reported as its error, not as a
// command failure.
func (t Tools) run(ctx context.Context, dir, bin string, args ...string) (stdout, stderr []byte, err error) {
	// command with captured output
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir

	var out bytes.Buffer
	var errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	// run
	err = cmd.Run()
	stdout = out.Bytes()
	stderr = errb.Bytes()
	if err == nil {
		return
	}

	// a finished context is not a command failure
	if ctx.Err() != nil {
		return stdout, stderr, ctx.Err()
	}

	return stdout, stderr, &CommandError{Bin: bin, Args: args, Stderr: t.tail(errb.String(), 4096), Err: err}
}

// tail — last n bytes of s, trimmed.
func (t Tools) tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

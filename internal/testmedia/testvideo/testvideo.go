// Package testvideo generates small media files with known properties for
// the bee video tests with ffmpeg lavfi (testsrc2, sine, color): no fixtures
// in the repository. A test that needs one is skipped only when ffmpeg is
// not installed at all.
package testvideo

import (
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Maker writes test media into Dir with the ffmpeg in PATH.
type Maker struct {
	T   testing.TB
	Dir string
}

// Clip — a test video: testsrc2 frames (a moving pattern with a frame
// counter), optionally with a 440 Hz sine track.
type Clip struct {
	Name     string  // file name in the directory: source.mp4
	Width    int     // coded width
	Height   int     // coded height
	FPS      int     // frame rate
	Seconds  float64 // duration
	Audio    bool    // a stereo 48 kHz AAC sine track as long as the video
	Rotation int     // display matrix rotation, counter-clockwise degrees (ffmpeg -display_rotation); 0 = none
	SAR      string  // sample aspect ratio "w:h" of anamorphic pixels; empty = square
	HDR      bool    // 10-bit HEVC tagged bt2020 / PQ (smpte2084) instead of 8-bit H.264
}

// Sound — a test audio file: a stereo sine tone at a known level, with
// optional full-scale clicks (a high true peak over a quiet loudness) and
// an optional step down to a tenth of the level half way (a wide loudness
// range).
type Sound struct {
	Name     string  // file name in the directory: sound.m4a, sound.mp3
	Seconds  float64 // duration
	Freq     int     // tone, Hz
	VolumeDB float64 // peak level of the tone, dBFS: 0 is full scale
	Clicks   bool    // a 1 ms click at 0.95 every half second
	Step     bool    // the second half 20 dB quieter
}

// Picture — a test still: a half transparent white rectangle (png keeps
// the alpha).
type Picture struct {
	Name   string // file name in the directory: mark.png
	Width  int
	Height int
}

// Clip — the clip written into Dir; its path.
func (a *Maker) Clip(c Clip) string {
	a.T.Helper()
	out := filepath.Join(a.Dir, c.Name)

	// the inputs
	args := []string{"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=%d:duration=%g", c.Width, c.Height, c.FPS, c.Seconds)}
	if c.Audio {
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%g", c.Seconds))
	}

	// square or anamorphic pixels
	if c.SAR != "" {
		args = append(args, "-vf", "setsar="+strings.ReplaceAll(c.SAR, ":", "/"))
	}

	// the codecs
	if c.HDR {
		args = append(args, "-c:v", "libx265", "-pix_fmt", "yuv420p10le", "-tag:v", "hvc1",
			"-x265-params", "log-level=error:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p")
	}
	if c.Audio {
		args = append(args, "-c:a", "aac", "-ac", "2")
	}

	// without rotation: written directly
	if c.Rotation == 0 {
		a.ffmpeg(append(args, out)...)
		return out
	}

	// with rotation: a display matrix set on a stream copy
	plain := filepath.Join(a.Dir, "plain-"+c.Name)
	a.ffmpeg(append(args, plain)...)
	a.ffmpeg("-display_rotation", fmt.Sprint(c.Rotation), "-i", plain, "-c", "copy", out)
	return out
}

// Sound — the sound written into Dir; its path.
func (a *Maker) Sound(s Sound) string {
	a.T.Helper()
	out := filepath.Join(a.Dir, s.Name)

	// the expression of one sample at time t
	expr := fmt.Sprintf("%g*sin(2*PI*%d*t)", math.Pow(10, s.VolumeDB/20), s.Freq)
	if s.Step {
		expr = fmt.Sprintf("%s*if(lt(t,%g),1,0.1)", expr, s.Seconds/2)
	}
	if s.Clicks {
		expr += "+if(lt(mod(t,0.5),0.001),0.95,0)"
	}

	source := fmt.Sprintf("aevalsrc=exprs='%s|%s':sample_rate=44100:duration=%g", expr, expr, s.Seconds)
	a.ffmpeg("-f", "lavfi", "-i", source, out)
	return out
}

// Picture — the picture written into Dir; its path.
func (a *Maker) Picture(p Picture) string {
	a.T.Helper()
	out := filepath.Join(a.Dir, p.Name)
	a.ffmpeg("-f", "lavfi", "-i", fmt.Sprintf("color=c=white@0.5:size=%dx%d:duration=1", p.Width, p.Height),
		"-frames:v", "1", "-vf", "format=rgba", "-update", "1", out)
	return out
}

// ffmpeg — one ffmpeg run; skips the test when ffmpeg is not installed,
// fails it on an error.
func (a *Maker) ffmpeg(args ...string) {
	a.T.Helper()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		a.T.Skip("ffmpeg is not installed")
	}

	args = append([]string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}, args...)
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		a.T.Fatalf("ffmpeg %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

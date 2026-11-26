// Package testaudio generates small audio files with known properties for
// bee tests (ffmpeg lavfi sine). Tests skip when ffmpeg is missing.
package testaudio

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Opts of a generated file.
type Opts struct {
	Name     string  // out file name with extension: song.mp3, song.flac, song.wav
	Seconds  float64 // default 5
	Channels int     // default 2
	VolumeDB float64 // gain on a full-scale sine: -20 is quiet, 0 is full scale
	Freq     int     // default 440
	Rate     int     // default 44100
	Title    string  // tag
	Artist   string  // tag
	Lead     float64 // seconds of silence prepended
	Trail    float64 // seconds of silence appended
	LeftOnly bool    // stereo with a digitally silent right channel
	Cover    bool    // embedded cover art: a 64x64 jpeg attached picture (mp3, flac, m4a)
}

// Available — ffmpeg in PATH.
func Available() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// Skip the test when ffmpeg is missing.
func Skip(t testing.TB) {
	t.Helper()
	if !Available() {
		t.Skip("ffmpeg is not installed")
	}
}

// Generate writes the file into dir and returns its path.
func Generate(t testing.TB, dir string, o Opts) string {
	t.Helper()
	Skip(t)

	// defaults
	if o.Seconds <= 0 {
		o.Seconds = 5
	}
	if o.Channels <= 0 {
		o.Channels = 2
	}
	if o.Freq <= 0 {
		o.Freq = 440
	}
	if o.Rate <= 0 {
		o.Rate = 44100
	}
	if o.Name == "" {
		o.Name = "song.mp3"
	}
	out := filepath.Join(dir, o.Name)

	// the sine source plays at -18 dBFS: +18 makes VolumeDB 0 full scale
	filters := []string{fmt.Sprintf("volume=%.2fdB", o.VolumeDB+18)}
	if o.LeftOnly {
		filters = append(filters, "pan=stereo|c0=c0|c1=0*c0")
	}
	if o.Lead > 0 {
		filters = append(filters, fmt.Sprintf("adelay=%d:all=1", int(o.Lead*1000)))
	}
	if o.Trail > 0 {
		filters = append(filters, fmt.Sprintf("apad=pad_dur=%.2f", o.Trail))
	}

	// the sine input
	sine := fmt.Sprintf("sine=frequency=%d:sample_rate=%d:duration=%.2f", o.Freq, o.Rate, o.Seconds)
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", sine}

	// the cover: a second input attached as a picture
	if o.Cover {
		args = append(args, "-f", "lavfi", "-i", "color=c=red:s=64x64:r=1:d=1",
			"-map", "0:a", "-map", "1:v", "-c:v", "mjpeg", "-disposition:v:0", "attached_pic", "-id3v2_version", "3")
	}

	// filters and channels
	args = append(args, "-af", strings.Join(filters, ","), "-ac", fmt.Sprint(o.Channels))

	// tags
	if o.Title != "" {
		args = append(args, "-metadata", "title="+o.Title)
	}
	if o.Artist != "" {
		args = append(args, "-metadata", "artist="+o.Artist)
	}

	// the codec by the extension
	switch strings.ToLower(filepath.Ext(o.Name)) {
	case ".mp3":
		args = append(args, "-c:a", "libmp3lame", "-b:a", "192k")
	case ".flac":
		args = append(args, "-c:a", "flac")
	case ".wav":
		args = append(args, "-c:a", "pcm_s16le")
	case ".m4a":
		args = append(args, "-c:a", "aac", "-b:a", "160k")
	case ".ogg":
		args = append(args, "-c:a", "libvorbis")
	}
	args = append(args, out)

	// run ffmpeg: a build without the encoder skips the test
	cmd := exec.Command("ffmpeg", args...)
	res, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("ffmpeg cannot generate %s: %v: %s", o.Name, err, res)
	}
	return out
}

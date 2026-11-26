// Package image makes the outputs of bee images with the vips command line
// tools: probe the source, decode it once into a normalized file, then
// one vips chain per output (resize, effects, encode), verified, with the
// colours and the thumbhash of the result.
package images

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	types "github.com/angels-you/bee/pkg/types/images"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// samples — a 16×12 file of every input format but generic heif,
// decoded by Check.
//
//go:embed samples
var samples embed.FS

// Tools — the vips binaries. Every run blocks the loaders vips marks
// untrusted (VIPS_BLOCK_UNTRUSTED: ImageMagick, raw, vips native …) and
// uses at most Threads threads (VIPS_CONCURRENCY).
type Tools struct {
	Vips       string // vips, a path or a name in PATH
	Header     string // vipsheader
	Threads    int    // threads of one vips run, at least 1
	AvifEffort int    // avif encoder effort, 0 (fastest, biggest) .. 9 (slowest, smallest)
}

// Version — vips answers (readiness).
func (a Tools) Version(ctx context.Context) error {
	_, err := a.run(ctx, a.Vips, "--version")
	return err
}

// Check — vips decodes a sample of every input format and encodes and
// reads back every output format; dir is a scratch directory. Run once at
// start: a missing codec stops the worker instead of failing jobs.
func (a Tools) Check(ctx context.Context, dir string) error {
	tmp, err := os.MkdirTemp(dir, "check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	entries, err := samples.ReadDir("samples")
	if err != nil {
		return err
	}
	var errs []error
	inputs := map[Format]bool{}
	for _, e := range entries {
		data, err := samples.ReadFile("samples/" + e.Name())
		if err != nil {
			return err
		}
		file := filepath.Join(tmp, e.Name())
		if err = os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
		info, err := a.Probe(ctx, file)
		if err == nil {
			_, err = a.Normalize(ctx, file, info, filepath.Join(tmp, "norm-"+e.Name()+".tif"))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("vips cannot read %s: %w", e.Name(), err))
			continue
		}
		inputs[info.Format] = true
	}
	// heif is the heif container with a codec other than hevc or av1
	// (avc, jpeg): there is no sample, the decoders of heic and avif are
	// what real files need
	for _, f := range types.FormatIndexes() {
		if !inputs[f] && f != Heif {
			errs = append(errs, fmt.Errorf("vips cannot read %s", f))
		}
	}

	src := filepath.Join(tmp, "check.tif")
	if _, err = a.run(ctx, a.Vips, "black", src, "8", "8", "--bands", "3"); err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, f := range types.FormatOutputIndexes() {
		out := filepath.Join(tmp, "check."+f.String())
		if _, err = a.run(ctx, a.Vips, "copy", src, out); err == nil {
			var info Info
			if info, err = a.Probe(ctx, out); err == nil && info.Format != f {
				err = fmt.Errorf("reads back as %s", info.Format)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("vips cannot write %s: %w", f, err))
		}
	}
	return errors.Join(errs...)
}

// header — the fields vipsheader -a prints for a file, by name.
func (a Tools) header(ctx context.Context, file string) (map[string]string, error) {
	out, err := a.run(ctx, a.Header, "-a", file)
	if err != nil {
		return nil, err
	}
	fields := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 16<<20) // gif palettes and exif dumps are long lines
	for sc.Scan() {
		if name, value, ok := strings.Cut(sc.Text(), ": "); ok {
			fields[name] = value
		}
	}
	return fields, sc.Err()
}

// run — one vips process; the error carries the end of its stderr.
func (a Tools) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "VIPS_BLOCK_UNTRUSTED=1", "VIPS_CONCURRENCY="+strconv.Itoa(a.Threads))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.ReplaceAll(strings.TrimSpace(stderr.String()), "\n", "; ")
		if len(msg) > 300 { // the cause is printed last
			msg = msg[len(msg)-300:]
		}
		return nil, fmt.Errorf("%s %s: %w: %s", filepath.Base(bin), args[0], err, msg)
	}
	return stdout.Bytes(), nil
}

package images

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/angels-you/bee/pkg/types/code"
)

// Output — one verified output file.
type Output struct {
	File    string
	Format  Format
	Width   int
	Height  int
	Size    int64
	Quality int // the encode quality, 0 for png and lossless
	SHA256  string
	Colors  Colors // what the profile asked for
}

// step — one vips run: vips <op> <inputs…> <out> <args…>. An input is
// an image by index: 0 is the source of the chain, i+1 the output of
// step i.
type step struct {
	op   string
	in   []int
	args []string
}

// Render — one output of p from the normalized source, written to
// <dir>/<name>.<format>: one vips run when the profile has no effects,
// more runs for the effects and the contain padding (the vips command
// line does one operation per run, see steps); the last run encodes.
// With MaxBytes the chain ends in an uncompressed tiff and only the
// encode is repeated while the quality steps down to minQuality. The
// output is verified before it is returned. maxSide bounds the output
// sides.
func (a Tools) Render(ctx context.Context, src Source, p Profile, dir, name string, minQuality, maxSide int) (Output, error) {
	// output size within the limit
	g := p.Geometry(src.Width, src.Height)
	switch {
	case g.Width > maxSide:
		return Output{}, code.New(code.InvalidDimensions, fmt.Sprintf("output %dx%d is above %d", g.Width, g.Height, maxSide), nil)
	case g.Height > maxSide:
		return Output{}, code.New(code.InvalidDimensions, fmt.Sprintf("output %dx%d is above %d", g.Width, g.Height, maxSide), nil)
	}

	// the vips runs
	steps, err := a.steps(p, src, g)
	if err != nil {
		return Output{}, err
	}

	// the output file
	out := Output{File: filepath.Join(dir, name+"."+p.Format.String()), Format: p.Format, Width: g.Width, Height: g.Height}
	if p.Lossy() {
		out.Quality = p.Quality
	}

	// make it: straight into the format, or through a tiff fitted to MaxBytes
	if p.MaxBytes == 0 {
		err = a.chain(ctx, src.File, steps, out.File+a.save(p, out.Quality), dir, name)
		if err != nil {
			return Output{}, err
		}
	} else {
		pre := filepath.Join(dir, name+"-pre.tif")
		err = a.chain(ctx, src.File, steps, pre, dir, name)
		if err != nil {
			return Output{}, err
		}

		out.Quality, err = a.fit(ctx, pre, out.File, p, minQuality)
		if err != nil {
			return Output{}, err
		}
	}

	// check what was written
	err = a.verify(ctx, out)
	if err != nil {
		return Output{}, err
	}

	// size and hash
	err = out.digest()
	if err != nil {
		return Output{}, code.New(code.InternalTemporaryError, "output unreadable", err)
	}

	// colours, when asked
	if a.wantsColors(p) {
		out.Colors, err = a.colors(ctx, out.File, filepath.Join(dir, name+"-small.png"), p)
		if err != nil {
			return Output{}, err
		}
	}

	return out, nil
}

// wantsColors — p asks for the thumbhash or any colour.
func (a Tools) wantsColors(p Profile) bool {
	switch {
	case p.Thumbhash:
		return true
	case p.DominantColor:
		return true
	case p.AverageColor:
		return true
	case p.PaletteColors > 0:
		return true
	}

	return false
}

// steps — the vips runs that turn the normalized source into the output
// picture, in order:
//
//	resize      thumbnail (and the cover crop)
//	premultiply with alpha, before blur or sharpen: no dark fringes
//	blur        gaussblur, sigma Blur × BlurSigma of the longer side
//	sharpen     blurred = gaussblur(src, radius), detail = src − blurred,
//	            src + (Sharpness − 1) × detail
//	unpremultiply
//	to 8 bit    linear: × Brightness on the colour bands, rounded, clipped
//	padding     contain: bandjoin_const (alpha for transparent padding), gravity
//
// Only the runs the profile needs: no effects is the resize alone.
func (a Tools) steps(p Profile, src Source, g Geometry) ([]step, error) {
	// resize
	steps := []step{a.resize(p, g)}

	// effects
	steps = a.effects(steps, p, src, g)

	// padding: contain only
	if p.Fit != Contain {
		return steps, nil
	}

	return a.padding(steps, p, src, g)
}

// resize — the thumbnail run: forced to the picture size, or the cover crop.
func (a Tools) resize(p Profile, g Geometry) step {
	w := strconv.Itoa(g.ImageWidth)
	h := strconv.Itoa(g.ImageHeight)

	// not cover: the exact picture size
	if p.Fit != Cover {
		return step{op: "thumbnail", in: []int{0}, args: []string{w, "--height", h, "--size", "force"}}
	}

	// cover: crop the most interesting part, or the centre
	crop := "attention"
	if p.Center {
		crop = "centre"
	}

	return step{op: "thumbnail", in: []int{0}, args: []string{w, "--height", h, "--crop", crop}}
}

// effects — steps with the blur, sharpen and brightness runs of p added.
func (a Tools) effects(steps []step, p Profile, src Source, g Geometry) []step {
	side := float64(max(g.ImageWidth, g.ImageHeight))

	// what the profile asks for: 0 and 1 are no change
	sharpen := p.Sharpness > 0 && p.Sharpness != 1
	brightness := p.Brightness > 0 && p.Brightness != 1

	// blur and sharpen work on premultiplied colour when there is alpha
	premultiplied := src.Bands == 4 && (p.Blur > 0 || sharpen)

	// premultiply
	if premultiplied {
		steps = append(steps, step{op: "premultiply", in: []int{len(steps)}, args: []string{"--max-alpha", "255"}})
	}

	// blur
	if p.Blur > 0 {
		sigma := strconv.FormatFloat(p.Blur*a.blurSigma()*side, 'f', -1, 64)
		steps = append(steps, step{op: "gaussblur", in: []int{len(steps)}, args: []string{sigma, "--min-ampl", a.gaussMinAmpl(), "--precision", "approximate"}})
	}

	// sharpen: the picture to sharpen, then its blur, the detail, the scaled detail
	if sharpen {
		sharp := len(steps)
		sigma := strconv.FormatFloat(max(a.sharpenMinSigma(), a.sharpenSigma()*side), 'f', -1, 64)
		factor := strconv.FormatFloat(p.Sharpness-1, 'f', -1, 64)

		steps = append(steps,
			step{op: "gaussblur", in: []int{sharp}, args: []string{sigma, "--min-ampl", a.gaussMinAmpl(), "--precision", "float"}},
			step{op: "subtract", in: []int{sharp, sharp + 1}},
			step{op: "linear", in: []int{sharp + 2}, args: []string{"--", factor, "0"}}, // "--": the factor is negative below 1
			step{op: "add", in: []int{sharp, sharp + 3}},
		)
	}

	// unpremultiply
	if premultiplied {
		steps = append(steps, step{op: "unpremultiply", in: []int{len(steps)}, args: []string{"--max-alpha", "255"}})
	}

	// back to 8 bit, with the brightness: needed after any effect
	toByte := premultiplied || sharpen || brightness
	if !toByte {
		return steps
	}

	b := "1"
	if brightness {
		b = strconv.FormatFloat(p.Brightness, 'f', -1, 64)
	}

	// the alpha band is kept as is
	mul, add := b, "0.5"
	if src.Bands == 4 {
		mul = b + " " + b + " " + b + " 1"
		add = "0.5 0.5 0.5 0.5"
	}

	return append(steps, step{op: "linear", in: []int{len(steps)}, args: []string{mul, add, "--uchar"}})
}

// padding — steps with the contain padding added: an alpha band when the
// padding is transparent, then the picture centred on the output size.
func (a Tools) padding(steps []step, p Profile, src Source, g Geometry) ([]step, error) {
	// padding colour; jpg has no transparency
	bg, err := p.Background()
	if err != nil {
		return nil, code.New(code.InvalidJob, err.Error(), nil)
	}

	if p.Format == Jpg {
		bg = a.flatten(p)
	}

	// transparent padding needs an alpha band
	bands := src.Bands
	needsAlpha := bands == 3 && bg.A < 255
	if needsAlpha {
		steps = append(steps, step{op: "bandjoin_const", in: []int{len(steps)}, args: []string{"255"}})
		bands = 4
	}

	// the colour of every band
	ink := fmt.Sprintf("%d %d %d", bg.R, bg.G, bg.B)
	if bands == 4 {
		ink += " " + strconv.Itoa(int(bg.A))
	}

	// centre the picture
	steps = append(steps, step{op: "gravity", in: []int{len(steps)}, args: []string{"centre", strconv.Itoa(g.Width), strconv.Itoa(g.Height), "--extend", "background", "--background", ink}})
	return steps, nil
}

// save — the vips save options of the output: no metadata at all; jpg
// flattens transparency onto BackgroundColor (black when empty); avif is
// 8-bit, the widest supported depth, encoded with effort (vips 0..9).
func (a Tools) save(p Profile, quality int) string {
	// avif effort: from the job profile, else the bee default
	effort := a.AvifEffort
	if p.Effort != nil {
		effort = *p.Effort
	}

	// no metadata, the quality when there is one
	opts := []string{"keep=none"}
	if quality > 0 {
		opts = append(opts, "Q="+strconv.Itoa(quality))
	}

	// options of the format
	switch p.Format {
	case Jpg:
		bg := a.flatten(p)
		opts = append(opts, fmt.Sprintf("background=%d %d %d", bg.R, bg.G, bg.B))
	case Webp:
		opts = append(opts, "lossless="+strconv.FormatBool(p.Lossless))
	case Avif:
		opts = append(opts, "lossless="+strconv.FormatBool(p.Lossless), "bitdepth=8", "effort="+strconv.Itoa(effort))
	}

	return "[" + strings.Join(opts, ",") + "]"
}

// flatten — the opaque colour jpg puts under transparency and uses for
// padding: BackgroundColor without its alpha, black when it is empty.
func (a Tools) flatten(p Profile) Color {
	bg, _ := p.Background() // validated with the job; empty is transparent black
	bg.A = 255
	return bg
}

// chain — runs the steps from src; the last writes target (a file with
// save options), the others write uncompressed tiffs in dir. An
// intermediate is removed after the last step that reads it.
func (a Tools) chain(ctx context.Context, src string, steps []step, target, dir, name string) error {
	// the step that reads image i last
	last := make([]int, len(steps)+1)
	for i, s := range steps {
		for _, k := range s.in {
			last[k] = i
		}
	}

	images := []string{src}
	for i, s := range steps {
		// the last step writes the target, the others a tiff
		out := target
		if i < len(steps)-1 {
			out = filepath.Join(dir, name+"-"+strconv.Itoa(i)+".tif")
		}

		// vips <op> <inputs…> <out> <args…>
		args := []string{s.op}
		for _, k := range s.in {
			args = append(args, images[k])
		}
		args = append(args, out)
		args = append(args, s.args...)

		// run
		_, err := a.run(ctx, a.Vips, args...)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			return code.New(code.InternalTemporaryError, "vips "+s.op+" failed", err)
		}
		images = append(images, out)

		// remove intermediates no later step reads
		for k := 1; k <= i; k++ {
			if last[k] == i {
				os.Remove(images[k])
			}
		}
	}

	return nil
}

// fit — encodes pre into file within p.MaxBytes: at p.Quality when it
// fits, else the highest quality from minQuality up that fits (binary
// search: a few encodes). Png and lossless have no quality to give: they
// fit or fail. Returns the quality used, 0 without one.
func (a Tools) fit(ctx context.Context, pre, file string, p Profile, minQuality int) (int, error) {
	// no quality: fits or fails
	if !p.Lossy() {
		return 0, a.fitLossless(ctx, pre, file, p)
	}

	// the asked quality
	fits, err := a.encode(ctx, pre, file, p, p.Quality)
	if err != nil {
		return p.Quality, err
	}

	if fits {
		return p.Quality, nil
	}

	// low fits (checked next), high does not
	low, high := minQuality, p.Quality
	if low >= high {
		return 0, code.New(code.OutputTooLarge, fmt.Sprintf("%s at quality %d is above %d bytes", p.Format, p.Quality, p.MaxBytes), nil)
	}

	// the lowest quality must fit
	fits, err = a.encode(ctx, pre, file, p, low)
	if err != nil {
		return 0, err
	}

	if !fits {
		return 0, code.New(code.OutputTooLarge, fmt.Sprintf("%s at quality %d is above %d bytes", p.Format, low, p.MaxBytes), nil)
	}

	// binary search between them
	last := low
	for high-low > 1 {
		q := (low + high) / 2
		fits, err = a.encode(ctx, pre, file, p, q)
		if err != nil {
			return 0, err
		}

		last = q
		if fits {
			low = q
		} else {
			high = q
		}
	}

	// the file holds the last try: encode the answer again when it differs
	if last != low {
		_, err = a.encode(ctx, pre, file, p, low)
		if err != nil {
			return 0, err
		}
	}

	return low, nil
}

// fitLossless — encodes pre into file without a quality; too big is
// OUTPUT_TOO_LARGE.
func (a Tools) fitLossless(ctx context.Context, pre, file string, p Profile) error {
	fits, err := a.encode(ctx, pre, file, p, 0)
	if err != nil {
		return err
	}

	if !fits {
		return code.New(code.OutputTooLarge, fmt.Sprintf("%s is above %d bytes", p.Format, p.MaxBytes), nil)
	}

	return nil
}

// encode — pre to file at quality; fits is the size within p.MaxBytes.
func (a Tools) encode(ctx context.Context, pre, file string, p Profile, quality int) (fits bool, err error) {
	// encode
	_, err = a.run(ctx, a.Vips, "copy", pre, file+a.save(p, quality))
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}

		return false, code.New(code.InternalTemporaryError, "vips encode failed", err)
	}

	// size
	st, err := os.Stat(file)
	if err != nil {
		return false, code.New(code.InternalTemporaryError, "output missing", err)
	}

	return st.Size() <= int64(p.MaxBytes), nil
}

// verify — the output reads back in its format and size and carries no
// metadata (exif, xmp, iptc, icc, comments).
func (a Tools) verify(ctx context.Context, out Output) error {
	// header
	h, err := a.header(ctx, out.File)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return code.New(code.InternalTemporaryError, "output unreadable", err)
	}

	// format and size
	loader := map[Format]string{Jpg: "jpegload", Png: "pngload", Webp: "webpload", Avif: "heifload"}[out.Format]
	notFormat := code.New(code.InternalTemporaryError, "output is not "+out.Format.String(), nil)
	wrongSize := code.New(code.InternalTemporaryError, fmt.Sprintf("output is %sx%s, expected %dx%d", h["width"], h["height"], out.Width, out.Height), nil)
	switch {
	case h["vips-loader"] != loader:
		return notFormat
	case out.Format == Avif && h["heif-compression"] != "av1":
		return notFormat
	case h["width"] != strconv.Itoa(out.Width):
		return wrongSize
	case h["height"] != strconv.Itoa(out.Height):
		return wrongSize
	}

	// no metadata field
	for name := range h {
		for _, meta := range []string{"exif-", "xmp-", "iptc-", "icc-", "comment"} {
			if strings.Contains(name, meta) {
				return code.New(code.InternalTemporaryError, "output keeps metadata "+name, nil)
			}
		}
	}

	return nil
}

// digest — Size and SHA256 of the file.
func (a *Output) digest() error {
	f, err := os.Open(a.File)
	if err != nil {
		return err
	}
	defer f.Close()

	// size and hash in one read
	h := sha256.New()
	a.Size, err = io.Copy(h, f)
	if err != nil {
		return err
	}

	a.SHA256 = hex.EncodeToString(h.Sum(nil))
	return nil
}

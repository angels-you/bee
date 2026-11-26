package images

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/angels-you/bee/pkg/types/code"
)

// Profile describes how to make one result from the original. Empty
// processing parameters (Fit, Effort, MinQuality) come from the bee
// defaults (env). Brightness, Sharpness and Blur are percent-style
// multipliers: 1 = 100 %, 1.5 = 150 %, 0.9 = 90 %; their radii are fractions
// of the result's long side, so the effect looks the same at any size
type Profile struct {
	Format Format `json:"format"`        // result: "jpg", "png", "webp" or "avif"
	Fit    Fit    `json:"fit,omitempty"` // "inside", "cover", "contain" or "fill", see Geometry; empty — inside

	// Quality 1..100 for jpg and lossy webp / avif; not needed for png and
	// lossless (may be 0)
	Quality   int  `json:"quality"`
	MaxWidth  int  `json:"max_width"`  // box width, px; 0 — unlimited (inside only)
	MaxHeight int  `json:"max_height"` // box height, px; 0 — unlimited (inside only)
	Center    bool `json:"center"`     // cover crop: true — center, false — the most interesting area (vips attention)

	// BackgroundColor is #rgb, #rrggbb or #rrggbbaa. The contain background
	// and what jpg flattens transparency onto. Empty: contain — transparent
	// background (black in jpg), jpg — onto black
	BackgroundColor string `json:"background_color"`

	// Blur is the gaussian blur strength: 0 — none, 1 = 100 %, 0.5 = 50 %;
	// up to MaxBlur
	Blur float64 `json:"blur"`

	// Brightness is the color channel multiplier, alpha unchanged: 1 — no
	// change, 1.5 = 150 %; 0 (not set) — no change; up to MaxBrightness
	Brightness float64 `json:"brightness"`

	// Sharpness is an unsharp mask: 1 — no change, 1.5 — fine detail at
	// 150 % (sharper), 0.9 — at 90 % (softer); 0 (not set) — no change; up
	// to MaxSharpness
	Sharpness float64 `json:"sharpness"`

	Upscale       bool `json:"upscale"`        // the original may be enlarged up to the box
	MaxBytes      int  `json:"max_bytes"`      // result size limit, bytes, 0 — none; lossy quality drops down to MinQuality
	Thumbhash     bool `json:"thumbhash"`      // TaskResult.Thumbhash of the result
	Lossless      bool `json:"lossless"`       // lossless webp / avif; png is always lossless, jpg never
	DominantColor bool `json:"dominant_color"` // TaskResult.DominantColor of the result
	PaletteColors int  `json:"palette_colors"` // TaskResult.Palette of the result, 0..MaxPalette colors
	AverageColor  bool `json:"average_color"`  // TaskResult.AverageColor of the result

	// MinQuality is the quality max_bytes never goes below, 1..100; 0 —
	// BEE_IMAGES_MIN_QUALITY
	MinQuality int `json:"min_quality,omitempty"`

	// Effort is the avif encoder effort, 0..9: higher is slower and smaller;
	// nil — BEE_IMAGES_AVIF_EFFORT (0 is a meaningful value, hence a pointer)
	Effort *int `json:"effort,omitempty"`
}

// Color is a straight (not premultiplied) rgba
type Color struct {
	R, G, B, A uint8
}

// Hex returns #rrggbb, alpha dropped
func (a Color) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", a.R, a.G, a.B)
}

// MaxBrightness is the Brightness limit: 300 %, channels × 3
func (a Profile) MaxBrightness() float64 {
	return 3
}

// MaxSharpness is the Sharpness limit: 300 %, fine detail × 3
func (a Profile) MaxSharpness() float64 {
	return 3
}

// MaxBlur is the Blur limit: 300 %, three times the sigma of Blur 1
func (a Profile) MaxBlur() float64 {
	return 3
}

// MaxPalette is the PaletteColors limit, colors
func (a Profile) MaxPalette() int {
	return 16
}

// FitOrInside returns Fit, inside when empty
func (a Profile) FitOrInside() Fit {
	if a.Fit == "" {
		return Inside
	}

	return a.Fit
}

// Validate checks all fields are within their limits. Bee limits (the
// longest side) are checked by the engine
func (a Profile) Validate() *code.Error {
	fit := a.FitOrInside()

	// format and fit
	switch {
	case !a.Format.IsOutput():
		return code.New(code.InvalidJob, fmt.Sprintf("format %q is not jpg, png, webp or avif", string(a.Format)), nil)
	case !fit.Valid():
		return code.New(code.InvalidJob, fmt.Sprintf("fit %q is not inside, cover, contain or fill", string(a.Fit)), nil)
	case a.Lossless && a.Format == Jpg:
		return code.New(code.InvalidJob, "jpg has no lossless mode", nil)
	}

	// quality
	switch {
	case a.Quality < 0, a.Quality > 100:
		return code.New(code.InvalidQuality, "quality must be within 0..100", nil)
	case a.Quality == 0 && a.Lossy():
		return code.New(code.InvalidQuality, "quality is required for a lossy "+a.Format.String(), nil)
	case a.MinQuality < 0, a.MinQuality > 100:
		return code.New(code.InvalidQuality, "min_quality must be within 0..100", nil)
	}

	// box
	switch {
	case a.MaxWidth < 0, a.MaxHeight < 0:
		return code.New(code.InvalidDimensions, "max_width and max_height must not be negative", nil)
	case fit != Inside && (a.MaxWidth == 0 || a.MaxHeight == 0):
		return code.New(code.InvalidDimensions, fit.String()+" needs max_width and max_height", nil)
	}

	// effects: !(x >= 0 && x <= max) also catches NaN
	switch {
	case !(a.Blur >= 0 && a.Blur <= a.MaxBlur()):
		return code.New(code.InvalidJob, fmt.Sprintf("blur must be within 0..%v", a.MaxBlur()), nil)
	case !(a.Sharpness >= 0 && a.Sharpness <= a.MaxSharpness()):
		return code.New(code.InvalidJob, fmt.Sprintf("sharpness must be within 0..%v", a.MaxSharpness()), nil)
	case !(a.Brightness >= 0 && a.Brightness <= a.MaxBrightness()):
		return code.New(code.InvalidJob, fmt.Sprintf("brightness must be within 0..%v", a.MaxBrightness()), nil)
	}

	// the rest
	switch {
	case a.MaxBytes < 0:
		return code.New(code.InvalidJob, "max_bytes must not be negative", nil)
	case a.PaletteColors < 0, a.PaletteColors > a.MaxPalette():
		return code.New(code.InvalidJob, fmt.Sprintf("palette_colors must be within 0..%d", a.MaxPalette()), nil)
	case a.Effort != nil && (*a.Effort < 0 || *a.Effort > 9):
		return code.New(code.InvalidJob, "effort must be within 0..9", nil)
	}

	// background color
	_, err := a.Background()
	if err != nil {
		return code.New(code.InvalidJob, err.Error(), nil)
	}

	return nil
}

// Background parses BackgroundColor: #rgb, #rrggbb or #rrggbbaa, alpha 255
// when not set; empty — transparent
func (a Profile) Background() (Color, error) {
	s := a.BackgroundColor
	if s == "" {
		return Color{}, nil
	}

	// no hash — not a color
	hexa, ok := strings.CutPrefix(s, "#")
	if !ok {
		return Color{}, fmt.Errorf("background_color %q is not #rgb, #rrggbb or #rrggbbaa", s)
	}

	// #rgb → #rrggbb
	if len(hexa) == 3 {
		hexa = string([]byte{hexa[0], hexa[0], hexa[1], hexa[1], hexa[2], hexa[2]})
	}

	// #rrggbb → #rrggbbff
	if len(hexa) == 6 {
		hexa += "ff"
	}

	b, err := hex.DecodeString(hexa)
	if err != nil || len(b) != 4 {
		return Color{}, fmt.Errorf("background_color %q is not #rgb, #rrggbb or #rrggbbaa", s)
	}

	return Color{R: b[0], G: b[1], B: b[2], A: b[3]}, nil
}

// Lossy reports whether encoding uses Quality: jpg, and webp / avif without
// lossless
func (a Profile) Lossy() bool {
	switch {
	case a.Format == Jpg:
		return true
	case a.Format == Png:
		return false
	}

	return !a.Lossless
}

// Geometry returns the result size for an original of w×h (already rotated).
//
//	inside   fitted into the box, aspect ratio kept; a 0 axis is unlimited,
//	         no box — the original size
//	cover    exactly the box: cover it, crop the excess; without Upscale an
//	         original smaller than the box is not enlarged: the result is
//	         the largest crop with the box aspect ratio
//	contain  exactly the box: the image fitted (like inside), centered,
//	         the rest filled with BackgroundColor
//	fill     exactly the box, stretched; without Upscale each side is no
//	         larger than the original's side
//
// Without Upscale inside and contain do not enlarge the original
func (a Profile) Geometry(w, h int) Geometry {
	bw := float64(a.MaxWidth)
	bh := float64(a.MaxHeight)
	fw := float64(w)
	fh := float64(h)

	switch a.FitOrInside() {
	case Cover:
		return a.cover(bw, bh, fw, fh)
	case Fill:
		return a.fill(w, h)
	}

	// inside and contain scale
	s := math.Inf(1)
	if a.MaxWidth > 0 {
		s = bw / fw
	}
	if a.MaxHeight > 0 {
		s = math.Min(s, bh/fh)
	}

	switch {
	case math.IsInf(s, 1):
		s = 1
	case !a.Upscale && s > 1:
		s = 1
	}

	iw := max(1, int(math.Round(fw*s)))
	ih := max(1, int(math.Round(fh*s)))

	if a.Fit == Contain {
		return Geometry{Width: a.MaxWidth, Height: a.MaxHeight, ImageWidth: iw, ImageHeight: ih}
	}

	return Geometry{Width: iw, Height: ih, ImageWidth: iw, ImageHeight: ih}
}

// cover returns the whole box or, without Upscale, the largest crop of its
// aspect ratio inside the original
func (a Profile) cover(bw, bh, fw, fh float64) Geometry {
	if a.Upscale || (bw <= fw && bh <= fh) {
		return Geometry{Width: a.MaxWidth, Height: a.MaxHeight, ImageWidth: a.MaxWidth, ImageHeight: a.MaxHeight}
	}

	// below 1: the box does not fit into the original
	k := math.Min(fw/bw, fh/bh)
	cw := max(1, int(math.Round(bw*k)))
	ch := max(1, int(math.Round(bh*k)))

	return Geometry{Width: cw, Height: ch, ImageWidth: cw, ImageHeight: ch}
}

// fill returns the box, without Upscale no larger than the original on each
// side
func (a Profile) fill(w, h int) Geometry {
	if a.Upscale {
		return Geometry{Width: a.MaxWidth, Height: a.MaxHeight, ImageWidth: a.MaxWidth, ImageHeight: a.MaxHeight}
	}

	sw := min(a.MaxWidth, w)
	sh := min(a.MaxHeight, h)

	return Geometry{Width: sw, Height: sh, ImageWidth: sw, ImageHeight: sh}
}

// Geometry is the size of one result: the result is Width×Height, the image
// inside it ImageWidth×ImageHeight (smaller only for contain, the rest is
// background)
type Geometry struct {
	Width, Height           int
	ImageWidth, ImageHeight int
}

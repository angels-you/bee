package images

import (
	"context"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/angels-you/bee/pkg/types/code"
)

// colorsSide — the colours and the thumbhash are measured on a copy of
// the output that fits in this box (thumbhash accepts at most 100×100).
const colorsSide = 100

// Colors of an output; every field only when its Profile flag asks for it.
type Colors struct {
	Thumbhash []byte   // ThumbHash of the output
	Dominant  string   // #rrggbb, the most common colour
	Palette   []string // #rrggbb, up to Profile.PaletteColors colours, most common first
	Average   string   // #rrggbb, the mean colour, weighted by alpha
}

// colors — what p asks for, measured on a small copy of the output
// written to small (png). Transparent pixels count by their alpha; an
// output without a visible pixel has no colours.
func (a Tools) colors(ctx context.Context, file, small string, p Profile) (Colors, error) {
	side := strconv.Itoa(colorsSide)
	if _, err := a.run(ctx, a.Vips, "thumbnail", file, small, side, "--height", side, "--size", "down"); err != nil {
		if ctx.Err() != nil {
			return Colors{}, ctx.Err()
		}
		return Colors{}, code.New(code.InternalTemporaryError, "output cannot be decoded", err)
	}
	f, err := os.Open(small)
	if err != nil {
		return Colors{}, code.New(code.InternalTemporaryError, "small copy missing", err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return Colors{}, code.New(code.InternalTemporaryError, "small copy unreadable", err)
	}
	b := img.Bounds()
	px := pixels{w: b.Dx(), h: b.Dy(), rgba: make([]color.NRGBA, 0, b.Dx()*b.Dy())}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			px.rgba = append(px.rgba, color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA))
		}
	}

	var c Colors
	if p.Thumbhash {
		c.Thumbhash = px.thumbhash()
	}
	if p.AverageColor {
		c.Average = px.average()
	}
	if p.DominantColor {
		c.Dominant = px.dominant()
	}
	if p.PaletteColors > 0 {
		c.Palette = px.palette(p.PaletteColors)
	}
	return c, nil
}

// pixels — straight rgba, row by row.
type pixels struct {
	w, h int
	rgba []color.NRGBA
}

// average — the mean colour weighted by alpha; empty without a visible pixel.
func (a pixels) average() string {
	var r, g, b, w float64
	for _, c := range a.rgba {
		k := float64(c.A)
		r, g, b, w = r+k*float64(c.R), g+k*float64(c.G), b+k*float64(c.B), w+k
	}
	if w == 0 {
		return ""
	}
	return Color{R: uint8(math.Round(r / w)), G: uint8(math.Round(g / w)), B: uint8(math.Round(b / w))}.Hex()
}

// dominant — the most common colour: pixels are counted (by alpha) in
// 16×16×16 colour cells, the answer is the mean colour of the fullest
// cell; empty without a visible pixel.
func (a pixels) dominant() string {
	var cells [4096]struct{ r, g, b, w float64 }
	best := -1
	for _, c := range a.rgba {
		if c.A == 0 {
			continue
		}
		i := int(c.R>>4)<<8 | int(c.G>>4)<<4 | int(c.B>>4)
		k := float64(c.A)
		cells[i].r += k * float64(c.R)
		cells[i].g += k * float64(c.G)
		cells[i].b += k * float64(c.B)
		cells[i].w += k
		if best < 0 || cells[i].w > cells[best].w {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	c := cells[best]
	return Color{R: uint8(math.Round(c.r / c.w)), G: uint8(math.Round(c.g / c.w)), B: uint8(math.Round(c.b / c.w))}.Hex()
}

// palette — up to n colours by median cut over the pixels at least half
// opaque: the box with the widest channel range is split at its median
// until there are n boxes or every box is one colour; each colour is the
// mean of its box, the fullest box first.
func (a pixels) palette(n int) []string {
	var visible box
	for _, c := range a.rgba {
		if c.A >= 128 {
			visible.colors = append(visible.colors, c)
		}
	}
	if len(visible.colors) == 0 {
		return nil
	}
	boxes := []box{visible}
	for len(boxes) < n {
		widest, span := -1, 0
		for i := range boxes {
			boxes[i].measure()
			if boxes[i].span > span {
				widest, span = i, boxes[i].span
			}
		}
		if widest < 0 {
			break
		}
		sort.Sort(&boxes[widest])
		cut := boxes[widest].cut()
		boxes = append(boxes, box{colors: boxes[widest].colors[cut:]})
		boxes[widest] = box{colors: boxes[widest].colors[:cut]}
	}
	sort.Stable(byPopulation(boxes))
	out := make([]string, len(boxes))
	for i, b := range boxes {
		out[i] = b.mean().Hex()
	}
	return out
}

// box — colours of one median cut box; channel and span are its widest
// channel (0 r, 1 g, 2 b) and that channel's range, set by measure.
type box struct {
	colors  []color.NRGBA
	channel int
	span    int
}

// measure — the widest channel and its range.
func (a *box) measure() {
	a.channel, a.span = 0, 0
	for ch := range 3 {
		lo, hi := 255, 0
		for _, c := range a.colors {
			v := a.value(c, ch)
			lo, hi = min(lo, v), max(hi, v)
		}
		if hi-lo > a.span {
			a.channel, a.span = ch, hi-lo
		}
	}
}

// cut — where the sorted box splits: the change of value nearest to the
// median, so one colour never ends up in two boxes (a box with a span has
// one).
func (a *box) cut() int {
	n, mid := len(a.colors), len(a.colors)/2
	for d := range n {
		for _, i := range [2]int{mid + d, mid - d} {
			if i > 0 && i < n && a.value(a.colors[i-1], a.channel) != a.value(a.colors[i], a.channel) {
				return i
			}
		}
	}
	return mid
}

// value — channel ch of c.
func (a *box) value(c color.NRGBA, ch int) int {
	switch ch {
	case 0:
		return int(c.R)
	case 1:
		return int(c.G)
	}
	return int(c.B)
}

// mean — the mean colour of the box.
func (a box) mean() Color {
	var r, g, b int
	for _, c := range a.colors {
		r, g, b = r+int(c.R), g+int(c.G), b+int(c.B)
	}
	k := float64(len(a.colors))
	return Color{R: uint8(math.Round(float64(r) / k)), G: uint8(math.Round(float64(g) / k)), B: uint8(math.Round(float64(b) / k))}
}

// sort.Interface over the widest channel, for the median split.
func (a *box) Len() int { return len(a.colors) }
func (a *box) Less(i, j int) bool {
	return a.value(a.colors[i], a.channel) < a.value(a.colors[j], a.channel)
}
func (a *box) Swap(i, j int) { a.colors[i], a.colors[j] = a.colors[j], a.colors[i] }

// byPopulation — boxes, the fullest first.
type byPopulation []box

func (a byPopulation) Len() int           { return len(a) }
func (a byPopulation) Less(i, j int) bool { return len(a[i].colors) > len(a[j].colors) }
func (a byPopulation) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

// thumbhash — the ThumbHash of the pixels (at most 100×100), the
// reference algorithm of github.com/evanw/thumbhash: the image composited
// over its average colour, converted to LPQA, DCT coefficients quantized
// into nibbles after a 5 byte header (6 with alpha).
func (a pixels) thumbhash() []byte {
	n := a.w * a.h
	var avgR, avgG, avgB, avgA float64
	for _, c := range a.rgba {
		alpha := float64(c.A) / 255
		avgR += alpha / 255 * float64(c.R)
		avgG += alpha / 255 * float64(c.G)
		avgB += alpha / 255 * float64(c.B)
		avgA += alpha
	}
	if avgA > 0 {
		avgR, avgG, avgB = avgR/avgA, avgG/avgA, avgB/avgA
	}

	hasAlpha := avgA < float64(n)
	limit := 7.0 // fewer luminance coefficients with alpha
	if hasAlpha {
		limit = 5
	}
	long := float64(max(a.w, a.h))
	lx := max(1, int(math.Round(limit*float64(a.w)/long)))
	ly := max(1, int(math.Round(limit*float64(a.h)/long)))

	l, p, q, al := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	for i, c := range a.rgba {
		alpha := float64(c.A) / 255
		r := avgR*(1-alpha) + alpha/255*float64(c.R)
		g := avgG*(1-alpha) + alpha/255*float64(c.G)
		b := avgB*(1-alpha) + alpha/255*float64(c.B)
		l[i], p[i], q[i], al[i] = (r+g+b)/3, (r+g)/2-b, r-g, alpha
	}

	lDC, lAC, lScale := a.dct(l, max(3, lx), max(3, ly))
	pDC, pAC, pScale := a.dct(p, 3, 3)
	qDC, qAC, qScale := a.dct(q, 3, 3)

	header24 := int(math.Round(63*lDC)) | int(math.Round(31.5+31.5*pDC))<<6 | int(math.Round(31.5+31.5*qDC))<<12 | int(math.Round(31*lScale))<<18
	short, landscape := lx, 0 // the coefficient count of the short side
	if a.w > a.h {
		short, landscape = ly, 1
	}
	header16 := short | int(math.Round(63*pScale))<<3 | int(math.Round(63*qScale))<<9 | landscape<<15
	if hasAlpha {
		header24 |= 1 << 23
	}
	hash := []byte{byte(header24), byte(header24 >> 8), byte(header24 >> 16), byte(header16), byte(header16 >> 8)}
	acs := [][]float64{lAC, pAC, qAC}
	if hasAlpha {
		aDC, aAC, aScale := a.dct(al, 5, 5)
		hash = append(hash, byte(int(math.Round(15*aDC))|int(math.Round(15*aScale))<<4))
		acs = append(acs, aAC)
	}
	index := 0
	for _, ac := range acs {
		for _, f := range ac {
			if index%2 == 0 {
				hash = append(hash, 0)
			}
			hash[len(hash)-1] |= byte(int(math.Round(15*f)) << ((index & 1) << 2))
			index++
		}
	}
	return hash
}

// dct — the DC term, the AC terms (mapped into 0..1) and their scale for
// one channel: coefficients cx < nx, cy < ny in the triangle cx·ny <
// nx·(ny-cy).
func (a pixels) dct(channel []float64, nx, ny int) (dc float64, ac []float64, scale float64) {
	fx := make([]float64, a.w)
	for cy := 0; cy < ny; cy++ {
		for cx := 0; cx*ny < nx*(ny-cy); cx++ {
			for x := range a.w {
				fx[x] = math.Cos(math.Pi / float64(a.w) * float64(cx) * (float64(x) + 0.5))
			}
			f := 0.0
			for y := range a.h {
				fy := math.Cos(math.Pi / float64(a.h) * float64(cy) * (float64(y) + 0.5))
				for x := range a.w {
					f += channel[x+y*a.w] * fx[x] * fy
				}
			}
			f /= float64(a.w * a.h)
			if cx > 0 || cy > 0 {
				ac = append(ac, f)
				scale = math.Max(scale, math.Abs(f))
			} else {
				dc = f
			}
		}
	}
	if scale > 0 {
		for i := range ac {
			ac[i] = 0.5 + 0.5/scale*ac[i]
		}
	}
	return dc, ac, scale
}

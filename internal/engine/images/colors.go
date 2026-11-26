package images

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/angels-you/bee/pkg/types/code"
)

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
	// small copy of the output
	side := strconv.Itoa(a.colorsSide())
	_, err := a.run(ctx, a.Vips, "thumbnail", file, small, side, "--height", side, "--size", "down")
	if err != nil {
		if ctx.Err() != nil {
			return Colors{}, ctx.Err()
		}

		return Colors{}, code.New(code.InternalTemporaryError, "output cannot be decoded", err)
	}

	// decode the copy
	f, err := os.Open(small)
	if err != nil {
		return Colors{}, code.New(code.InternalTemporaryError, "small copy missing", err)
	}

	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return Colors{}, code.New(code.InternalTemporaryError, "small copy unreadable", err)
	}

	// straight rgba pixels
	px := a.toPixels(img)

	// only what the profile asks for
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

// toPixels — the pixels of img as straight rgba, row by row.
func (a Tools) toPixels(img image.Image) pixels {
	b := img.Bounds()
	px := pixels{w: b.Dx(), h: b.Dy(), rgba: make([]color.NRGBA, 0, b.Dx()*b.Dy())}

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			px.rgba = append(px.rgba, c)
		}
	}

	return px
}

// pixels — straight rgba, row by row.
type pixels struct {
	w, h int
	rgba []color.NRGBA
}

// average — the mean colour weighted by alpha; empty without a visible pixel.
func (a pixels) average() string {
	// sums weighted by alpha
	var r, g, b, w float64
	for _, c := range a.rgba {
		k := float64(c.A)
		r += k * float64(c.R)
		g += k * float64(c.G)
		b += k * float64(c.B)
		w += k
	}

	// no visible pixel
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
		// invisible pixels do not count
		if c.A == 0 {
			continue
		}

		// the cell of the pixel: the high 4 bits of each channel
		i := int(c.R>>4)<<8 | int(c.G>>4)<<4 | int(c.B>>4)

		// count it by its alpha
		k := float64(c.A)
		cells[i].r += k * float64(c.R)
		cells[i].g += k * float64(c.G)
		cells[i].b += k * float64(c.B)
		cells[i].w += k

		// the fullest cell so far
		switch {
		case best < 0:
			best = i
		case cells[i].w > cells[best].w:
			best = i
		}
	}

	// no visible pixel
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
	// the pixels at least half opaque
	var visible box
	for _, c := range a.rgba {
		if c.A >= 128 {
			visible.colors = append(visible.colors, c)
		}
	}

	if len(visible.colors) == 0 {
		return nil
	}

	// median cut
	boxes := []box{visible}
	for len(boxes) < n {
		// the box with the widest channel
		widest, span := -1, 0
		for i := range boxes {
			boxes[i].measure()
			if boxes[i].span > span {
				widest = i
				span = boxes[i].span
			}
		}

		// every box is one colour
		if widest < 0 {
			break
		}

		// split it at the median of that channel
		sort.Sort(&boxes[widest])
		cut := boxes[widest].cut()
		boxes = append(boxes, box{colors: boxes[widest].colors[cut:]})
		boxes[widest] = box{colors: boxes[widest].colors[:cut]}
	}

	// the fullest box first
	sort.Stable(byPopulation(boxes))

	// mean colour of each box
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
	a.channel = 0
	a.span = 0

	for ch := range 3 {
		// range of the channel
		lo, hi := 255, 0
		for _, c := range a.colors {
			v := a.value(c, ch)
			lo = min(lo, v)
			hi = max(hi, v)
		}

		// wider than the widest so far
		if hi-lo > a.span {
			a.channel = ch
			a.span = hi - lo
		}
	}
}

// cut — where the sorted box splits: the change of value nearest to the
// median, so one colour never ends up in two boxes (a box with a span has
// one).
func (a *box) cut() int {
	n := len(a.colors)
	mid := n / 2

	// look both ways from the median, nearest first
	for d := range n {
		for _, i := range [2]int{mid + d, mid - d} {
			switch {
			case i <= 0:
				continue
			case i >= n:
				continue
			case a.value(a.colors[i-1], a.channel) == a.value(a.colors[i], a.channel):
				continue
			}

			return i
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
	// channel sums
	var r, g, b int
	for _, c := range a.colors {
		r += int(c.R)
		g += int(c.G)
		b += int(c.B)
	}

	k := float64(len(a.colors))
	return Color{R: uint8(math.Round(float64(r) / k)), G: uint8(math.Round(float64(g) / k)), B: uint8(math.Round(float64(b) / k))}
}

// Len — sort.Interface over the widest channel, for the median split.
func (a *box) Len() int {
	return len(a.colors)
}

// Less — sort.Interface: by the widest channel.
func (a *box) Less(i, j int) bool {
	return a.value(a.colors[i], a.channel) < a.value(a.colors[j], a.channel)
}

// Swap — sort.Interface.
func (a *box) Swap(i, j int) {
	a.colors[i], a.colors[j] = a.colors[j], a.colors[i]
}

// byPopulation — boxes, the fullest first.
type byPopulation []box

// Len — sort.Interface.
func (a byPopulation) Len() int {
	return len(a)
}

// Less — sort.Interface: more colours first.
func (a byPopulation) Less(i, j int) bool {
	return len(a[i].colors) > len(a[j].colors)
}

// Swap — sort.Interface.
func (a byPopulation) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

// thumbhash — the ThumbHash of the pixels (at most 100×100), the
// reference algorithm of github.com/evanw/thumbhash: the image composited
// over its average colour, converted to LPQA, DCT coefficients quantized
// into nibbles after a 5 byte header (6 with alpha).
func (a pixels) thumbhash() []byte {
	n := a.w * a.h

	// average colour, weighted by alpha
	var avgR, avgG, avgB, avgA float64
	for _, c := range a.rgba {
		alpha := float64(c.A) / 255
		avgR += alpha / 255 * float64(c.R)
		avgG += alpha / 255 * float64(c.G)
		avgB += alpha / 255 * float64(c.B)
		avgA += alpha
	}

	if avgA > 0 {
		avgR = avgR / avgA
		avgG = avgG / avgA
		avgB = avgB / avgA
	}

	// fewer luminance coefficients with alpha
	hasAlpha := avgA < float64(n)
	limit := 7.0
	if hasAlpha {
		limit = 5
	}

	// luminance coefficients along each side
	long := float64(max(a.w, a.h))
	lx := max(1, int(math.Round(limit*float64(a.w)/long)))
	ly := max(1, int(math.Round(limit*float64(a.h)/long)))

	// LPQA channels of the pixels composited over the average colour
	l := make([]float64, n)
	p := make([]float64, n)
	q := make([]float64, n)
	al := make([]float64, n)
	for i, c := range a.rgba {
		alpha := float64(c.A) / 255
		r := avgR*(1-alpha) + alpha/255*float64(c.R)
		g := avgG*(1-alpha) + alpha/255*float64(c.G)
		b := avgB*(1-alpha) + alpha/255*float64(c.B)

		l[i] = (r + g + b) / 3
		p[i] = (r+g)/2 - b
		q[i] = r - g
		al[i] = alpha
	}

	// DCT of each channel
	lDC, lAC, lScale := a.dct(l, max(3, lx), max(3, ly))
	pDC, pAC, pScale := a.dct(p, 3, 3)
	qDC, qAC, qScale := a.dct(q, 3, 3)

	// first header: DC terms and the luminance scale
	header24 := int(math.Round(63 * lDC))
	header24 |= int(math.Round(31.5+31.5*pDC)) << 6
	header24 |= int(math.Round(31.5+31.5*qDC)) << 12
	header24 |= int(math.Round(31*lScale)) << 18
	if hasAlpha {
		header24 |= 1 << 23
	}

	// the coefficient count of the short side
	short, landscape := lx, 0
	if a.w > a.h {
		short = ly
		landscape = 1
	}

	// second header: the short side and the colour scales
	header16 := short
	header16 |= int(math.Round(63*pScale)) << 3
	header16 |= int(math.Round(63*qScale)) << 9
	header16 |= landscape << 15

	hash := []byte{byte(header24), byte(header24 >> 8), byte(header24 >> 16), byte(header16), byte(header16 >> 8)}
	acs := [][]float64{lAC, pAC, qAC}

	// alpha: one more header byte and its AC terms
	if hasAlpha {
		aDC, aAC, aScale := a.dct(al, 5, 5)
		hash = append(hash, byte(int(math.Round(15*aDC))|int(math.Round(15*aScale))<<4))
		acs = append(acs, aAC)
	}

	// AC terms, two nibbles per byte
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
			// cosines along x
			for x := range a.w {
				fx[x] = math.Cos(math.Pi / float64(a.w) * float64(cx) * (float64(x) + 0.5))
			}

			// the coefficient
			f := 0.0
			for y := range a.h {
				fy := math.Cos(math.Pi / float64(a.h) * float64(cy) * (float64(y) + 0.5))
				for x := range a.w {
					f += channel[x+y*a.w] * fx[x] * fy
				}
			}
			f /= float64(a.w * a.h)

			// the first coefficient is the DC term, the others are AC
			isDC := cx == 0 && cy == 0
			if isDC {
				dc = f
				continue
			}

			ac = append(ac, f)
			scale = math.Max(scale, math.Abs(f))
		}
	}

	// AC terms into 0..1
	if scale > 0 {
		for i := range ac {
			ac[i] = 0.5 + 0.5/scale*ac[i]
		}
	}

	return dc, ac, scale
}

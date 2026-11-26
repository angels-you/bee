package images

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	types "github.com/angels-you/bee/pkg/types/images"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/angels-you/bee/internal/testmedia/testimage"
	"github.com/angels-you/bee/pkg/types/code"
)

var tools = Tools{Vips: "vips", Header: "vipsheader", Threads: 2, AvifEffort: 4}

// codeOf — the code of err, empty when it is not a *code.Error.
func codeOf(err error) code.Code {
	var e *code.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestCheck(t *testing.T) {
	if err := tools.Check(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("local vips misses a format: %v", err)
	}
	if err := (Tools{Vips: "no-such-vips", Header: "vipsheader", Threads: 1}).Check(context.Background(), t.TempDir()); err == nil {
		t.Fatal("missing vips passed the check")
	}
}

// source — the photo converted to format f in dir (vips keeps its
// metadata unless told otherwise).
func source(t *testing.T, photo, dir string, f Format) string {
	t.Helper()
	out := filepath.Join(dir, "source."+f.String())
	switch f {
	case Heic:
		testimage.Vips(t, "copy", photo, out+"[compression=hevc]")
	case Avif:
		testimage.Vips(t, "copy", photo, out+"[bitdepth=8]")
	default:
		testimage.Vips(t, "copy", photo, out)
	}
	return out
}

// render — the source through Normalize and Render with p.
func render(t *testing.T, file string, p Profile) (Output, error) {
	t.Helper()
	dir := t.TempDir()
	info, err := tools.Probe(context.Background(), file)
	if err != nil {
		t.Fatalf("probe %s: %v", file, err)
	}
	src, err := tools.Normalize(context.Background(), file, info, filepath.Join(dir, "norm.tif"))
	if err != nil {
		t.Fatalf("normalize %s: %v", file, err)
	}
	return tools.Render(context.Background(), src, p, dir, "out", 30, 8000)
}

// loaders — the vips loader of every output format.
var loaders = map[Format]string{Jpg: "jpegload", Png: "pngload", Webp: "webpload", Avif: "heifload"}

func TestEveryInputToEveryOutput(t *testing.T) {
	photo := testimage.Photo(t, t.TempDir(), 320, 240)
	inputs := []Format{Jpg, Png, Webp, Avif, Heic, Tiff, Gif}
	for _, in := range inputs {
		file := source(t, photo, t.TempDir(), in)
		info, err := tools.Probe(context.Background(), file)
		if err != nil || info.Format != in || info.Width != 320 || info.Height != 240 {
			t.Fatalf("%s: probe %+v, %v", in, info, err)
		}
		for _, f := range types.FormatOutputIndexes() {
			p := Profile{Format: f, Fit: Inside, Quality: 70, MaxWidth: 160, MaxHeight: 160}
			out, err := render(t, file, p)
			if err != nil {
				t.Fatalf("%s → %s: %v", in, f, err)
			}
			h := testimage.Header(t, out.File)
			data, _ := os.ReadFile(out.File)
			sum := sha256.Sum256(data)
			switch {
			case out.Width != 160 || out.Height != 120 || h["width"] != "160" || h["height"] != "120":
				t.Errorf("%s → %s: %dx%d, header %sx%s", in, f, out.Width, out.Height, h["width"], h["height"])
			case h["vips-loader"] != loaders[f], f == Avif && h["heif-compression"] != "av1":
				t.Errorf("%s → %s: loader %s %s", in, f, h["vips-loader"], h["heif-compression"])
			case out.Size != int64(len(data)) || out.SHA256 != hex.EncodeToString(sum[:]):
				t.Errorf("%s → %s: size/sha mismatch", in, f)
			case f.Mime() != "image/"+map[Format]string{Jpg: "jpeg", Png: "png", Webp: "webp", Avif: "avif"}[f]:
				t.Errorf("%s: mime %s", f, f.Mime())
			}
		}
	}
	t.Logf("generic heif (avc / jpeg codec) is not covered: the local libheif has no encoder to make a sample")
}

func TestOrientation(t *testing.T) {
	dir := t.TempDir()
	split := testimage.Split(t, dir, "split", 40, 20, []int{255, 0, 0}, []int{0, 0, 255})
	for orientation, top := range map[int][]int{1: nil, 6: {255, 0, 0}, 8: {0, 0, 255}, 3: nil} {
		file := testimage.Metadata(t, split, filepath.Join(dir, "o"+strconv.Itoa(orientation)+".jpg"), orientation)
		info, err := tools.Probe(context.Background(), file)
		if err != nil || info.Orientation != orientation {
			t.Fatalf("orientation %d: probe %+v %v", orientation, info, err)
		}
		w, h := info.Upright()
		src, err := tools.Normalize(context.Background(), file, info, filepath.Join(dir, "n"+strconv.Itoa(orientation)+".tif"))
		if err != nil || src.Width != w || src.Height != h {
			t.Fatalf("orientation %d: %+v %v, upright %dx%d", orientation, src, err, w, h)
		}
		if top == nil { // not turned: left half red (1), or turned 180: right half red (3)
			left := testimage.Pixel(t, src.File, 5, 10)
			wantRed := orientation == 1
			if (left[0] > 200) != wantRed {
				t.Errorf("orientation %d: left pixel %v", orientation, left)
			}
			continue
		}
		if src.Width != 20 || src.Height != 40 {
			t.Fatalf("orientation %d: %dx%d, want 20x40", orientation, src.Width, src.Height)
		}
		got := testimage.Pixel(t, src.File, 10, 5)
		for i := range 3 {
			if math.Abs(float64(got[i]-top[i])) > 40 {
				t.Errorf("orientation %d: top pixel %v, want %v", orientation, got, top)
				break
			}
		}
	}
}

func TestMetadataStripped(t *testing.T) {
	dir := t.TempDir()
	jpg := testimage.Metadata(t, testimage.Photo(t, dir, 200, 150), filepath.Join(dir, "meta.jpg"), 1)
	h := testimage.Header(t, jpg)
	if h["exif-data"] == "" || h["xmp-data"] == "" || h["orientation"] != "1" {
		t.Fatalf("planting failed: %v", h)
	}
	gps := false
	for name := range h {
		gps = gps || strings.HasPrefix(name, "exif-ifd3-")
	}
	if !gps {
		t.Fatalf("no gps ifd in the planted exif: %v", h)
	}
	// the same metadata carried into the other input formats (vips keeps it)
	sources := []string{jpg}
	for _, f := range []Format{Png, Webp, Avif, Tiff} {
		out := filepath.Join(dir, "meta."+f.String())
		testimage.Vips(t, "copy", jpg, out)
		if mh := testimage.Header(t, out); mh["exif-data"] == "" && mh["xmp-data"] == "" { // tiffsave keeps xmp, not exif
			t.Fatalf("%s source carries no metadata", f)
		}
		sources = append(sources, out)
	}
	for _, src := range sources {
		for _, f := range types.FormatOutputIndexes() {
			out, err := render(t, src, Profile{Format: f, Fit: Inside, Quality: 80, MaxWidth: 100})
			if err != nil {
				t.Fatalf("%s → %s: %v", src, f, err)
			}
			for name := range testimage.Header(t, out.File) {
				for _, meta := range []string{"exif", "xmp", "iptc", "icc", "comment", "orientation"} {
					if strings.Contains(name, meta) {
						t.Errorf("%s → %s keeps %s", filepath.Base(src), f, name)
					}
				}
			}
			data, _ := os.ReadFile(out.File)
			for _, marker := range []string{testimage.GPSMarker, testimage.XMPMarker, testimage.CommentMarker, "Exif\x00", "http://ns.adobe.com/xap"} {
				if bytes.Contains(data, []byte(marker)) {
					t.Errorf("%s → %s: bytes contain %q", filepath.Base(src), f, marker)
				}
			}
		}
	}
}

func TestAlpha(t *testing.T) {
	dir := t.TempDir()
	// left half transparent green, right half opaque blue
	png := filepath.Join(dir, "alpha.png")
	testimage.Vips(t, "copy", testimage.Split(t, dir, "alpha", 40, 20, []int{0, 255, 0, 0}, []int{0, 0, 255, 255}), png)

	for bg, want := range map[string][]int{"": {0, 0, 0}, "#ff0000": {255, 0, 0}} {
		out, err := render(t, png, Profile{Format: Jpg, Fit: Inside, Quality: 95, BackgroundColor: bg})
		if err != nil {
			t.Fatal(err)
		}
		got := testimage.Pixel(t, out.File, 5, 10)
		if len(got) != 3 || absDiff(got, want) > 12 {
			t.Errorf("jpg background %q: transparent area %v, want %v", bg, got, want)
		}
		if right := testimage.Pixel(t, out.File, 35, 10); absDiff(right, []int{0, 0, 255}) > 12 {
			t.Errorf("jpg background %q: opaque area %v", bg, right)
		}
	}
	for _, f := range []Format{Png, Webp, Avif} {
		out, err := render(t, png, Profile{Format: f, Fit: Inside, Quality: 90, Lossless: f != Png})
		if err != nil {
			t.Fatal(err)
		}
		left, right := testimage.Pixel(t, out.File, 5, 10), testimage.Pixel(t, out.File, 35, 10)
		if len(left) != 4 || left[3] != 0 || right[3] != 255 {
			t.Errorf("%s: alpha lost: left %v right %v", f, left, right)
		}
	}
}

func TestContain(t *testing.T) {
	dir := t.TempDir()
	photo := testimage.Solid(t, dir, "wide", 200, 100, 0, 0, 255)
	out, err := render(t, photo, Profile{Format: Png, Fit: Contain, MaxWidth: 100, MaxHeight: 100})
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != 100 || out.Height != 100 {
		t.Fatalf("contain %dx%d", out.Width, out.Height)
	}
	if corner := testimage.Pixel(t, out.File, 0, 0); len(corner) != 4 || corner[3] != 0 {
		t.Errorf("png padding is not transparent: %v", corner)
	}
	if mid := testimage.Pixel(t, out.File, 50, 50); absDiff(mid[:3], []int{0, 0, 255}) > 2 || mid[3] != 255 {
		t.Errorf("png picture: %v", mid)
	}
	out, err = render(t, photo, Profile{Format: Jpg, Quality: 95, Fit: Contain, MaxWidth: 100, MaxHeight: 100, BackgroundColor: "#00ff00"})
	if err != nil {
		t.Fatal(err)
	}
	if corner := testimage.Pixel(t, out.File, 0, 0); absDiff(corner, []int{0, 255, 0}) > 12 {
		t.Errorf("jpg padding: %v", corner)
	}
	out, err = render(t, photo, Profile{Format: Webp, Lossless: true, Fit: Contain, MaxWidth: 100, MaxHeight: 100, BackgroundColor: "#ff000080"})
	if err != nil {
		t.Fatal(err)
	}
	if corner := testimage.Pixel(t, out.File, 0, 0); absDiff(corner, []int{255, 0, 0, 128}) > 1 {
		t.Errorf("webp padding: %v", corner)
	}
}

func TestEveryFitThroughVips(t *testing.T) {
	photo := testimage.Photo(t, t.TempDir(), 300, 200)
	for _, fit := range types.FitIndexes() {
		for _, up := range []bool{false, true} {
			for _, box := range [][2]int{{120, 120}, {600, 100}, {500, 500}} {
				p := Profile{Format: Webp, Quality: 60, Fit: fit, MaxWidth: box[0], MaxHeight: box[1], Upscale: up, Center: up}
				out, err := render(t, photo, p)
				want := p.Geometry(300, 200)
				if err != nil || out.Width != want.Width || out.Height != want.Height {
					t.Errorf("%s upscale %v box %v: %dx%d %v, want %+v", fit, up, box, out.Width, out.Height, err, want)
				}
			}
		}
	}
}

func TestAnimatedFirstFrame(t *testing.T) {
	dir := t.TempDir()
	gif := testimage.Animated(t, dir, "anim", testimage.Solid(t, dir, "red", 40, 30, 255, 0, 0), testimage.Solid(t, dir, "blue", 40, 30, 0, 0, 255))
	info, err := tools.Probe(context.Background(), gif)
	if err != nil || info.Pages != 2 || info.Width != 40 || info.Height != 30 {
		t.Fatalf("probe %+v %v", info, err)
	}
	out, err := render(t, gif, Profile{Format: Png, Fit: Inside})
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != 40 || out.Height != 30 {
		t.Fatalf("first frame %dx%d", out.Width, out.Height)
	}
	if px := testimage.Pixel(t, out.File, 20, 15); absDiff(px[:3], []int{255, 0, 0}) > 2 {
		t.Errorf("not the first frame: %v", px)
	}
}

func TestBrokenSources(t *testing.T) {
	dir := t.TempDir()
	jpg := filepath.Join(dir, "good.jpg")
	testimage.Vips(t, "copy", testimage.Photo(t, dir, 400, 300), jpg)
	data, _ := os.ReadFile(jpg)
	files := map[string][]byte{
		"truncated.jpg": data[:len(data)/2],
		"header.jpg":    data[:600],
		"garbage.bin":   bytes.Repeat([]byte("not an image "), 100),
		"empty":         nil,
		"matrix.mat":    []byte("2 2\n1 2\n3 4\n"),
	}
	want := map[string]code.Code{
		"truncated.jpg": code.InvalidImage,
		"header.jpg":    code.InvalidImage,
		"garbage.bin":   code.InvalidImage,
		"empty":         code.InvalidImage,
		"matrix.mat":    code.UnsupportedFormat,
	}
	for name, body := range files {
		file := filepath.Join(dir, name)
		os.WriteFile(file, body, 0o644)
		info, err := tools.Probe(context.Background(), file)
		if err == nil {
			_, err = tools.Normalize(context.Background(), file, info, filepath.Join(dir, name+".tif"))
		}
		if got := codeOf(err); got != want[name] {
			t.Errorf("%s: %v, want %s", name, err, want[name])
		}
	}
}

func TestMaxBytes(t *testing.T) {
	photo := testimage.Photo(t, t.TempDir(), 400, 300)
	s90, s30 := sizeAt(t, photo, 90), sizeAt(t, photo, 30)
	if s30 >= s90 {
		t.Fatalf("quality does not change the size: %d %d", s30, s90)
	}
	limit := int((s30 + s90) / 2)
	out, err := render(t, photo, Profile{Format: Webp, Fit: Inside, Quality: 90, MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if out.Quality >= 90 || out.Quality < 30 || out.Size > int64(limit) {
		t.Errorf("stepping: quality %d size %d, limit %d", out.Quality, out.Size, limit)
	}
	if next := sizeAt(t, photo, out.Quality+1); next <= int64(limit) && out.Quality+1 < 90 {
		t.Logf("quality %d also fits (%d): sizes are not strictly monotonic", out.Quality+1, next)
	}
	// fits at once: the asked quality is kept
	if out, err = render(t, photo, Profile{Format: Webp, Fit: Inside, Quality: 90, MaxBytes: int(s90)}); err != nil || out.Quality != 90 {
		t.Errorf("fitting output stepped: %d %v", out.Quality, err)
	}
	for _, p := range []Profile{
		{Format: Webp, Fit: Inside, Quality: 90, MaxBytes: 100},    // below the min quality size
		{Format: Jpg, Fit: Inside, Quality: 20, MaxBytes: 100},     // quality already below min
		{Format: Png, Fit: Inside, MaxBytes: 100},                  // nothing to step
		{Format: Webp, Fit: Inside, Lossless: true, MaxBytes: 100}, // nothing to step
	} {
		if _, err = render(t, photo, p); codeOf(err) != code.OutputTooLarge {
			t.Errorf("%+v: %v, want OUTPUT_TOO_LARGE", p, err)
		}
	}
}

func TestDimensionLimit(t *testing.T) {
	photo := testimage.Photo(t, t.TempDir(), 300, 200)
	dir := t.TempDir()
	info, _ := tools.Probe(context.Background(), photo)
	src, err := tools.Normalize(context.Background(), photo, info, filepath.Join(dir, "n.tif"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tools.Render(context.Background(), src, Profile{Format: Png, Fit: Inside}, dir, "o", 30, 250); codeOf(err) != code.InvalidDimensions {
		t.Errorf("source size above the max side: %v", err)
	}
}

func TestEffects(t *testing.T) {
	dir := t.TempDir()
	gray := testimage.Solid(t, dir, "gray", 64, 64, 64, 64, 64)
	for b, want := range map[float64]int{2: 128, 0.9: 58, 1.5: 96, 1: 64, 0: 64, 3: 192} {
		out, err := render(t, gray, Profile{Format: Png, Fit: Inside, Brightness: b})
		if err != nil {
			t.Fatal(err)
		}
		if px := testimage.Pixel(t, out.File, 32, 32); absDiff(px, []int{want, want, want}) > 0 {
			t.Errorf("brightness %v of 64: %v, want %d", b, px, want)
		}
	}
	alpha := testimage.Solid(t, dir, "alpha", 64, 64, 64, 64, 64, 100)
	out, err := render(t, alpha, Profile{Format: Png, Fit: Inside, Brightness: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if px := testimage.Pixel(t, out.File, 32, 32); absDiff(px, []int{32, 32, 32, 100}) > 0 {
		t.Errorf("brightness keeps alpha: %v", px)
	}

	photo := testimage.Photo(t, dir, 300, 200)
	plain, err := render(t, photo, Profile{Format: Png, Fit: Contain, MaxWidth: 200, MaxHeight: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Profile{
		{Format: Png, Fit: Contain, MaxWidth: 200, MaxHeight: 200, Blur: 1},
		{Format: Png, Fit: Contain, MaxWidth: 200, MaxHeight: 200, Sharpness: 2},
		{Format: Png, Fit: Contain, MaxWidth: 200, MaxHeight: 200, Blur: 0.5, Sharpness: 1.5, Brightness: 1.2},
	} {
		out, err := render(t, photo, p)
		if err != nil || out.Width != 200 || out.Height != 200 || out.SHA256 == plain.SHA256 {
			t.Errorf("%+v: %dx%d %v (unchanged: %v)", p, out.Width, out.Height, err, out.SHA256 == plain.SHA256)
		}
		if corner := testimage.Pixel(t, out.File, 0, 0); corner[3] != 0 {
			t.Errorf("%+v: effects reached the padding: %v", p, corner)
		}
	}
	// 1 and 0 (not set) are 100 %: the picture is the plain one
	for _, p := range []Profile{{Sharpness: 1, Brightness: 1}, {Sharpness: 0, Brightness: 0, Blur: 0}} {
		p.Format, p.Fit, p.MaxWidth, p.MaxHeight = Png, Contain, 200, 200
		if out, err := render(t, photo, p); err != nil || out.SHA256 != plain.SHA256 {
			t.Errorf("%+v changed the picture: %v", p, err)
		}
	}
}

// edge — a w×h grey picture, 64 on the left half, 192 on the right, and
// the row through its middle after p (inside, png, the same size).
func edge(t *testing.T, w, h int, p Profile) []int {
	t.Helper()
	dir := t.TempDir()
	src := testimage.Split(t, dir, "edge", w, h, []int{64, 64, 64}, []int{192, 192, 192})
	p.Format, p.Fit = Png, Inside
	out, err := render(t, src, p)
	if err != nil {
		t.Fatal(err)
	}
	img := testimage.Decode(t, out.File)
	row := make([]int, w)
	for x := range row {
		row[x] = int(img.NRGBAAt(x, h/2).R)
	}
	return row
}

// TestBlurScale — Blur 1 is a gaussian of sigma BlurSigma of the longer
// side: across a 64 | 192 edge the value at distance d is 64 + 128 ×
// Φ(d / sigma); 1.5 blurs wider than 1 and 0.5 narrower; the same
// picture at double size has the same profile at the same relative
// position.
func TestBlurScale(t *testing.T) {
	const w, h = 400, 40
	sigma := tools.blurSigma() * w
	one := edge(t, w, h, Profile{Blur: 1})
	for _, x := range []int{w/2 - 30, w/2 - 13, w/2 - 1, w/2 + 6, w/2 + 24} {
		d := float64(x) + 0.5 - w/2 // from the edge to the centre of pixel x
		if want := 64 + 128*0.5*math.Erfc(-d/sigma/math.Sqrt2); math.Abs(float64(one[x])-want) > 1.5 {
			t.Errorf("blur 1 (sigma %.0f px) at %+.1f px from the edge: %d, want %.1f", sigma, d, one[x], want)
		}
	}
	if one[5] != 64 || one[w-5] != 192 {
		t.Errorf("blur 1 reached far from the edge: %d %d", one[5], one[w-5])
	}
	half, wide := edge(t, w, h, Profile{Blur: 0.5}), edge(t, w, h, Profile{Blur: 1.5})
	at := w/2 - int(sigma)
	if !(half[at] < one[at] && one[at] < wide[at]) {
		t.Errorf("blur strength is not monotonic at %d: 0.5 %d, 1 %d, 1.5 %d", at, half[at], one[at], wide[at])
	}
	double := edge(t, 2*w, h, Profile{Blur: 1})
	for _, x := range []int{w/2 - 20, w/2 - 10, w/2 - 3, w/2 + 6, w/2 + 15} {
		if d := one[x] - double[2*x]; d < -3 || d > 3 {
			t.Errorf("blur 1 at %d of %d: %d, at %d of %d: %d", x, w, one[x], 2*x, 2*w, double[2*x])
		}
	}
}

// TestSharpness — the unsharp mask in both directions: above 1 the edge
// overshoots (darker on the dark side, lighter on the light side), below
// 1 it softens (the two sides move towards each other); far from the
// edge nothing changes; more is more.
func TestSharpness(t *testing.T) {
	const w, h = 400, 40
	plain := edge(t, w, h, Profile{})
	more, most, less := edge(t, w, h, Profile{Sharpness: 1.5}), edge(t, w, h, Profile{Sharpness: 3}), edge(t, w, h, Profile{Sharpness: 0.5})
	l, r := w/2-1, w/2
	switch {
	case plain[l] != 64 || plain[r] != 192:
		t.Fatalf("plain edge %d %d", plain[l], plain[r])
	case !(more[l] < 64 && more[r] > 192):
		t.Errorf("sharpness 1.5 does not overshoot: %d | %d", more[l], more[r])
	case !(most[l] < more[l] && most[r] > more[r]):
		t.Errorf("sharpness %d is not sharper than 1.5: %d | %d vs %d | %d", 3, most[l], most[r], more[l], more[r])
	case !(less[l] > 64 && less[r] < 192):
		t.Errorf("sharpness 0.5 does not soften: %d | %d", less[l], less[r])
	}
	for _, row := range [][]int{more, most, less} {
		if row[10] != 64 || row[w-10] != 192 {
			t.Errorf("sharpness changed the flat areas: %d %d", row[10], row[w-10])
		}
	}
}

// TestEffectsOnAlpha — opaque red next to transparent black: blur and
// sharpen work on premultiplied colour, so the half transparent pixels
// at the edge stay red instead of darkening towards the black of the
// transparent ones; alpha itself is blurred.
func TestEffectsOnAlpha(t *testing.T) {
	src := testimage.Split(t, t.TempDir(), "red", 200, 20, []int{0, 0, 0, 0}, []int{255, 0, 0, 255})
	for _, p := range []Profile{{Format: Png, Fit: Inside, Blur: 1}, {Format: Png, Fit: Inside, Sharpness: 0.5}, {Format: Webp, Fit: Inside, Lossless: true, Blur: 0.5, Sharpness: 2}} {
		out, err := render(t, src, p)
		if err != nil {
			t.Fatal(err)
		}
		img, seen := testimage.Decode(t, out.File), 0
		for x := 60; x < 140; x++ {
			px := img.NRGBAAt(x, 10)
			if px.A < 5 || px.A > 250 {
				continue
			}
			seen++
			if px.R < 250 || px.G > 3 || px.B > 3 {
				t.Errorf("%+v: dark fringe at %d: %v", p, x, px)
			}
		}
		if seen == 0 {
			t.Errorf("%+v: alpha was not blurred", p)
		}
	}
}

func TestColors(t *testing.T) {
	dir := t.TempDir()
	red := testimage.Solid(t, dir, "red", 75, 100, 255, 0, 0)
	blue := testimage.Solid(t, dir, "blue", 25, 100, 0, 0, 255)
	file := filepath.Join(dir, "rb.png")
	testimage.Vips(t, "join", red, blue, file, "horizontal")

	out, err := render(t, file, Profile{Format: Png, Fit: Inside, Thumbhash: true, DominantColor: true, AverageColor: true, PaletteColors: 4})
	if err != nil {
		t.Fatal(err)
	}
	c := out.Colors
	if c.Dominant != "#ff0000" {
		t.Errorf("dominant %s", c.Dominant)
	}
	if c.Average != "#bf0040" {
		t.Errorf("average %s, want #bf0040", c.Average)
	}
	if !slices.Equal(c.Palette, []string{"#ff0000", "#0000ff"}) {
		t.Errorf("palette %v", c.Palette)
	}
	if len(c.Thumbhash) < 5 || c.Thumbhash[2]&0x80 != 0 {
		t.Fatalf("thumbhash %x", c.Thumbhash)
	}
	r, g, b := thumbhashAverage(c.Thumbhash)
	if math.Abs(r-0.75) > 0.05 || g > 0.05 || math.Abs(b-0.25) > 0.05 {
		t.Errorf("thumbhash average %.2f %.2f %.2f, want .75 0 .25", r, g, b)
	}

	none, err := render(t, file, Profile{Format: Png, Fit: Inside})
	if err != nil || none.Colors.Thumbhash != nil || none.Colors.Dominant != "" || none.Colors.Palette != nil || none.Colors.Average != "" {
		t.Errorf("colours without asking: %+v %v", none.Colors, err)
	}
}

func TestThumbhashAlpha(t *testing.T) {
	px := pixels{w: 4, h: 2}
	for i := range 8 {
		px.rgba = append(px.rgba, color.NRGBA{R: 255, A: uint8(255 * (i % 2))})
	}
	h := px.thumbhash()
	if h[2]&0x80 == 0 {
		t.Fatalf("alpha flag not set: %x", h)
	}
	if px.average() != "#ff0000" || px.dominant() != "#ff0000" {
		t.Errorf("transparent pixels counted: %s %s", px.average(), px.dominant())
	}
	clear := pixels{w: 1, h: 1, rgba: []color.NRGBA{{}}}
	if clear.average() != "" || clear.dominant() != "" || clear.palette(3) != nil {
		t.Error("colours of a fully transparent image")
	}
}

// thumbhashAverage — the average colour stored in a thumbhash header,
// the reference decoder's formula.
func thumbhashAverage(h []byte) (r, g, b float64) {
	header := int(h[0]) | int(h[1])<<8 | int(h[2])<<16
	l := float64(header&63) / 63
	p := float64(header>>6&63)/31.5 - 1
	q := float64(header>>12&63)/31.5 - 1
	b = l - 2.0/3*p
	r = (3*l - b + q) / 2
	g = r - q
	return r, g, b
}

// sizeAt — the size of the photo as webp at quality q.
func sizeAt(t *testing.T, photo string, q int) int64 {
	t.Helper()
	out, err := render(t, photo, Profile{Format: Webp, Fit: Inside, Quality: q})
	if err != nil {
		t.Fatal(err)
	}
	return out.Size
}

// absDiff — the largest difference of two pixels, band by band.
func absDiff(a, b []int) int {
	d := 0
	for i := range min(len(a), len(b)) {
		d = max(d, a[i]-b[i], b[i]-a[i])
	}
	return d
}

func TestColourspaces(t *testing.T) {
	dir := t.TempDir()
	photo := testimage.Photo(t, dir, 120, 80)
	grey, deepTIFF, deep, cmyk, p3 := filepath.Join(dir, "grey.png"), filepath.Join(dir, "deep.tif"), filepath.Join(dir, "deep.png"), filepath.Join(dir, "cmyk.jpg"), filepath.Join(dir, "p3.jpg")
	testimage.Vips(t, "colourspace", photo, grey, "b-w")
	testimage.Vips(t, "colourspace", photo, deepTIFF, "rgb16")
	testimage.Vips(t, "copy", deepTIFF, deep)
	testimage.Vips(t, "colourspace", photo, cmyk, "cmyk")
	testimage.Vips(t, "icc_transform", photo, p3, "p3")
	for _, file := range []string{grey, deep, cmyk, p3} {
		h := testimage.Header(t, file)
		info, err := tools.Probe(context.Background(), file)
		if err != nil {
			t.Fatal(err)
		}
		src, err := tools.Normalize(context.Background(), file, info, filepath.Join(dir, filepath.Base(file)+".norm.tif"))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		n := testimage.Header(t, src.File)
		if n["interpretation"] != "srgb" || n["format"] != "uchar" || src.Bands != 3 || n["icc-profile-data"] != "" {
			t.Errorf("%s (%s %s %s bands): normalized to %s %s %d bands, icc %q", filepath.Base(file), h["interpretation"], h["format"], h["bands"], n["interpretation"], n["format"], src.Bands, n["icc-profile-data"])
		}
	}
}

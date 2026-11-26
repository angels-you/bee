// Package testimage — images for the bee image tests, made on the fly with the
// local vips: no fixtures in the repository but the embedded samples of
// image.Check. A missing vips fails the test, it is never skipped.
package testimage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	_ "image/png" // Decode reads png
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Secret markers planted in the metadata of test sources: none of them
// may survive into an output.
const (
	GPSMarker     = "GinTestGPSArea"
	XMPMarker     = "GinTestXMPCreator"
	CommentMarker = "GinTestComment"
)

// Vips runs vips with args and fails the test on an error.
func Vips(t testing.TB, args ...string) string {
	t.Helper()

	// stdout is the answer; warnings of vips go to stderr and stay out of it
	var warnings bytes.Buffer
	cmd := exec.Command("vips", args...)
	cmd.Stderr = &warnings

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("vips %s: %v\n%s%s", strings.Join(args, " "), err, out, warnings.String())
	}

	return string(out)
}

// Header — the fields of vipsheader -a, by name.
func Header(t testing.TB, file string) map[string]string {
	t.Helper()

	// run vipsheader
	cmd := exec.Command("vipsheader", "-a", file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vipsheader %s: %v\n%s", file, err, out)
	}

	// "name: value" lines
	fields := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		fields[name] = value
	}
	return fields
}

// Pixel — the band values of file at x, y.
func Pixel(t testing.TB, file string, x, y int) []int {
	t.Helper()

	// one value per band, rounded
	var px []int
	point := Vips(t, "getpoint", file, strconv.Itoa(x), strconv.Itoa(y))
	for _, f := range strings.Fields(point) {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatalf("getpoint %s: %q", file, f)
		}
		px = append(px, int(v+0.5))
	}
	return px
}

// Decode — every pixel of file (any format vips reads), straight rgba:
// one vips run to a png in a temp dir, decoded by Go.
func Decode(t testing.TB, file string) *image.NRGBA {
	t.Helper()

	// vips writes a png
	png := filepath.Join(t.TempDir(), "decoded.png")
	Vips(t, "copy", file, png)

	// Go reads it
	f, err := os.Open(png)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}

	// straight rgba
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}

// Photo — a w×h sRGB tiff in dir: a colour gradient with noise, so lossy
// encoders have work to do and sizes depend on quality.
func Photo(t testing.TB, dir string, w, h int) string {
	t.Helper()
	tmp := t.TempDir()
	ws := strconv.Itoa(w)
	hs := strconv.Itoa(h)

	// the intermediate files
	xy := filepath.Join(tmp, "xy.tif")
	rg := filepath.Join(tmp, "rg.tif")
	r := filepath.Join(tmp, "r.tif")
	g := filepath.Join(tmp, "g.tif")
	s := filepath.Join(tmp, "s.tif")
	b := filepath.Join(tmp, "b.tif")
	rgb := filepath.Join(tmp, "rgb.tif")
	noise := filepath.Join(tmp, "n.tif")
	noisy := filepath.Join(tmp, "noisy.tif")
	u8 := filepath.Join(tmp, "u8.tif")

	// red and green: the x and y gradients scaled to 0..255
	Vips(t, "xyz", xy, ws, hs)
	Vips(t, "linear", xy, rg, fmt.Sprintf("%f %f", 255/float64(w), 255/float64(h)), "0 0")
	Vips(t, "extract_band", rg, r, "0")
	Vips(t, "extract_band", rg, g, "1")

	// blue: the mean of red and green
	Vips(t, "add", r, g, s)
	Vips(t, "linear", s, b, "0.5", "0")
	Vips(t, "bandjoin", r+" "+g+" "+b, rgb)

	// noise on top, cast to 8 bit
	Vips(t, "gaussnoise", noise, ws, hs, "--sigma", "24", "--mean", "0")
	Vips(t, "add", rgb, noise, noisy)
	Vips(t, "cast", noisy, u8, "uchar")

	// tagged sRGB
	out := filepath.Join(dir, "photo-"+ws+"x"+hs+".tif")
	Vips(t, "copy", u8, out, "--interpretation", "srgb")
	return out
}

// Solid — a w×h tiff of one colour; 4 bands (with alpha) when rgba has
// 4 values, else 3.
func Solid(t testing.TB, dir, name string, w, h int, rgba ...int) string {
	t.Helper()
	tmp := t.TempDir()
	black := filepath.Join(tmp, "black.tif")
	lin := filepath.Join(tmp, "lin.tif")

	// the linear transform: a zero factor and the colour as offset per band
	zeros := make([]string, len(rgba))
	ink := make([]string, len(rgba))
	for i, v := range rgba {
		zeros[i] = "0"
		ink[i] = strconv.Itoa(v)
	}

	// a black image painted with the colour
	Vips(t, "black", black, strconv.Itoa(w), strconv.Itoa(h), "--bands", strconv.Itoa(len(rgba)))
	Vips(t, "linear", black, lin, strings.Join(zeros, " "), strings.Join(ink, " "), "--uchar")

	// tagged sRGB
	out := filepath.Join(dir, name+".tif")
	Vips(t, "copy", lin, out, "--interpretation", "srgb")
	return out
}

// Split — a w×h tiff, the left half of colour left, the right half of
// colour right (same band count).
func Split(t testing.TB, dir, name string, w, h int, left, right []int) string {
	t.Helper()
	tmp := t.TempDir()

	// two halves joined side by side
	l := Solid(t, tmp, "left", w/2, h, left...)
	r := Solid(t, tmp, "right", w-w/2, h, right...)
	out := filepath.Join(dir, name+".tif")
	Vips(t, "join", l, r, out, "horizontal")
	return out
}

// Animated — a gif of the frames (same size), one page each.
func Animated(t testing.TB, dir, name string, frames ...string) string {
	t.Helper()

	// the frames stacked into one tall image
	tall := filepath.Join(t.TempDir(), "tall.tif")
	Vips(t, "arrayjoin", strings.Join(frames, " "), tall, "--across", "1")

	// cut into pages of one frame height
	h := Header(t, frames[0])["height"]
	out := filepath.Join(dir, name+".gif")
	Vips(t, "copy", tall, out+"[page-height="+h+"]")
	return out
}

// Metadata — src saved as a jpeg at dst with metadata planted right after
// SOI: an EXIF block (orientation, a GPS IFD with GPSMarker), an XMP
// packet with XMPMarker and a COM segment with CommentMarker.
func Metadata(t testing.TB, src, dst string, orientation int) string {
	t.Helper()

	// a clean jpeg
	Vips(t, "copy", src, dst+"[keep=none]")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}

	// the segments right after SOI
	m := &jpegMeta{orientation: orientation}
	var b bytes.Buffer
	b.Write(data[:2]) // SOI
	b.Write(m.segment(0xE1, m.exif()))
	b.Write(m.segment(0xE1, m.xmp()))
	b.Write(m.segment(0xFE, []byte(CommentMarker)))
	b.Write(data[2:])

	// written back
	err = os.WriteFile(dst, b.Bytes(), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// jpegMeta builds the metadata segments that Metadata plants into a jpeg.
type jpegMeta struct {
	orientation int // EXIF Orientation, 1..8
}

// segment — a jpeg marker segment.
func (a *jpegMeta) segment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}

// exif — "Exif\0\0" and a big endian tiff: IFD0 with Orientation and a
// GPS IFD pointer; the GPS IFD with GPSVersionID and GPSAreaInformation.
func (a *jpegMeta) exif() []byte {
	be := binary.BigEndian
	var b bytes.Buffer
	b.WriteString("MM\x00\x2a\x00\x00\x00\x08") // header, IFD0 at 8
	area := append([]byte("ASCII\x00\x00\x00"), GPSMarker...)

	// IFD0 at 8: 2 entries (2+24+4 = 30 bytes) → GPS IFD at 38
	entry := make([]byte, 12)
	b.Write([]byte{0, 2})

	// Orientation SHORT 1
	be.PutUint16(entry[0:], 0x0112)
	be.PutUint16(entry[2:], 3)
	be.PutUint32(entry[4:], 1)
	be.PutUint32(entry[8:], uint32(a.orientation)<<16)
	b.Write(entry)

	// GPSInfo LONG 1 → 38
	be.PutUint16(entry[0:], 0x8825)
	be.PutUint16(entry[2:], 4)
	be.PutUint32(entry[4:], 1)
	be.PutUint32(entry[8:], 38)
	b.Write(entry)
	b.Write([]byte{0, 0, 0, 0}) // no IFD1

	// GPS IFD at 38: 2 entries (30 bytes) → area data at 68
	b.Write([]byte{0, 2})

	// GPSVersionID BYTE 4 = 2.2.0.0
	be.PutUint16(entry[0:], 0x0000)
	be.PutUint16(entry[2:], 1)
	be.PutUint32(entry[4:], 4)
	copy(entry[8:], []byte{2, 2, 0, 0})
	b.Write(entry)

	// GPSAreaInformation UNDEFINED
	be.PutUint16(entry[0:], 0x001C)
	be.PutUint16(entry[2:], 7)
	be.PutUint32(entry[4:], uint32(len(area)))
	be.PutUint32(entry[8:], 68)
	b.Write(entry)
	b.Write([]byte{0, 0, 0, 0})

	// the area data
	b.Write(area)
	return append([]byte("Exif\x00\x00"), b.Bytes()...)
}

// xmp — the XMP namespace and a minimal packet naming XMPMarker as the
// creator.
func (a *jpegMeta) xmp() []byte {
	namespace := "http://ns.adobe.com/xap/1.0/\x00"
	packet := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:creator>` + XMPMarker + `</dc:creator></rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
	return []byte(namespace + packet)
}

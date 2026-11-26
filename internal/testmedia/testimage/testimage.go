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
	out, err := exec.Command("vips", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("vips %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// Header — the fields of vipsheader -a, by name.
func Header(t testing.TB, file string) map[string]string {
	t.Helper()
	out, err := exec.Command("vipsheader", "-a", file).CombinedOutput()
	if err != nil {
		t.Fatalf("vipsheader %s: %v\n%s", file, err, out)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if name, value, ok := strings.Cut(line, ": "); ok {
			fields[name] = value
		}
	}
	return fields
}

// Pixel — the band values of file at x, y.
func Pixel(t testing.TB, file string, x, y int) []int {
	t.Helper()
	var px []int
	for _, f := range strings.Fields(Vips(t, "getpoint", file, strconv.Itoa(x), strconv.Itoa(y))) {
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
	png := filepath.Join(t.TempDir(), "decoded.png")
	Vips(t, "copy", file, png)
	f, err := os.Open(png)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}

// Photo — a w×h sRGB tiff in dir: a colour gradient with noise, so lossy
// encoders have work to do and sizes depend on quality.
func Photo(t testing.TB, dir string, w, h int) string {
	t.Helper()
	tmp := t.TempDir()
	ws, hs := strconv.Itoa(w), strconv.Itoa(h)
	xy, rg := filepath.Join(tmp, "xy.tif"), filepath.Join(tmp, "rg.tif")
	r, g, s, b := filepath.Join(tmp, "r.tif"), filepath.Join(tmp, "g.tif"), filepath.Join(tmp, "s.tif"), filepath.Join(tmp, "b.tif")
	rgb, noise, noisy, u8 := filepath.Join(tmp, "rgb.tif"), filepath.Join(tmp, "n.tif"), filepath.Join(tmp, "noisy.tif"), filepath.Join(tmp, "u8.tif")
	Vips(t, "xyz", xy, ws, hs)
	Vips(t, "linear", xy, rg, fmt.Sprintf("%f %f", 255/float64(w), 255/float64(h)), "0 0")
	Vips(t, "extract_band", rg, r, "0")
	Vips(t, "extract_band", rg, g, "1")
	Vips(t, "add", r, g, s)
	Vips(t, "linear", s, b, "0.5", "0")
	Vips(t, "bandjoin", r+" "+g+" "+b, rgb)
	Vips(t, "gaussnoise", noise, ws, hs, "--sigma", "24", "--mean", "0")
	Vips(t, "add", rgb, noise, noisy)
	Vips(t, "cast", noisy, u8, "uchar")
	out := filepath.Join(dir, "photo-"+ws+"x"+hs+".tif")
	Vips(t, "copy", u8, out, "--interpretation", "srgb")
	return out
}

// Solid — a w×h tiff of one colour; 4 bands (with alpha) when rgba has
// 4 values, else 3.
func Solid(t testing.TB, dir, name string, w, h int, rgba ...int) string {
	t.Helper()
	tmp := t.TempDir()
	black, lin := filepath.Join(tmp, "black.tif"), filepath.Join(tmp, "lin.tif")
	zeros, ink := make([]string, len(rgba)), make([]string, len(rgba))
	for i, v := range rgba {
		zeros[i], ink[i] = "0", strconv.Itoa(v)
	}
	Vips(t, "black", black, strconv.Itoa(w), strconv.Itoa(h), "--bands", strconv.Itoa(len(rgba)))
	Vips(t, "linear", black, lin, strings.Join(zeros, " "), strings.Join(ink, " "), "--uchar")
	out := filepath.Join(dir, name+".tif")
	Vips(t, "copy", lin, out, "--interpretation", "srgb")
	return out
}

// Split — a w×h tiff, the left half of colour left, the right half of
// colour right (same band count).
func Split(t testing.TB, dir, name string, w, h int, left, right []int) string {
	t.Helper()
	tmp := t.TempDir()
	l := Solid(t, tmp, "left", w/2, h, left...)
	r := Solid(t, tmp, "right", w-w/2, h, right...)
	out := filepath.Join(dir, name+".tif")
	Vips(t, "join", l, r, out, "horizontal")
	return out
}

// Animated — a gif of the frames (same size), one page each.
func Animated(t testing.TB, dir, name string, frames ...string) string {
	t.Helper()
	tall := filepath.Join(t.TempDir(), "tall.tif")
	Vips(t, "arrayjoin", strings.Join(frames, " "), tall, "--across", "1")
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
	Vips(t, "copy", src, dst+"[keep=none]")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	b.Write(data[:2]) // SOI
	b.Write(segment(0xE1, exif(orientation)))
	b.Write(segment(0xE1, append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmp()...)))
	b.Write(segment(0xFE, []byte(CommentMarker)))
	b.Write(data[2:])
	if err = os.WriteFile(dst, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// segment — a jpeg marker segment.
func segment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}

// exif — "Exif\0\0" and a big endian tiff: IFD0 with Orientation and a
// GPS IFD pointer; the GPS IFD with GPSVersionID and GPSAreaInformation.
func exif(orientation int) []byte {
	be := binary.BigEndian
	var b bytes.Buffer
	b.WriteString("MM\x00\x2a\x00\x00\x00\x08") // header, IFD0 at 8
	area := append([]byte("ASCII\x00\x00\x00"), GPSMarker...)
	// IFD0 at 8: 2 entries (2+24+4 = 30 bytes) → GPS IFD at 38
	entry := make([]byte, 12)
	b.Write([]byte{0, 2})
	be.PutUint16(entry[0:], 0x0112) // Orientation SHORT 1
	be.PutUint16(entry[2:], 3)
	be.PutUint32(entry[4:], 1)
	be.PutUint32(entry[8:], uint32(orientation)<<16)
	b.Write(entry)
	be.PutUint16(entry[0:], 0x8825) // GPSInfo LONG 1 → 38
	be.PutUint16(entry[2:], 4)
	be.PutUint32(entry[4:], 1)
	be.PutUint32(entry[8:], 38)
	b.Write(entry)
	b.Write([]byte{0, 0, 0, 0}) // no IFD1
	// GPS IFD at 38: 2 entries (30 bytes) → area data at 68
	b.Write([]byte{0, 2})
	be.PutUint16(entry[0:], 0x0000) // GPSVersionID BYTE 4 = 2.2.0.0
	be.PutUint16(entry[2:], 1)
	be.PutUint32(entry[4:], 4)
	copy(entry[8:], []byte{2, 2, 0, 0})
	b.Write(entry)
	be.PutUint16(entry[0:], 0x001C) // GPSAreaInformation UNDEFINED
	be.PutUint16(entry[2:], 7)
	be.PutUint32(entry[4:], uint32(len(area)))
	be.PutUint32(entry[8:], 68)
	b.Write(entry)
	b.Write([]byte{0, 0, 0, 0})
	b.Write(area)
	return append([]byte("Exif\x00\x00"), b.Bytes()...)
}

// xmp — a minimal XMP packet naming XMPMarker as the creator.
func xmp() []byte {
	return []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:creator>` + XMPMarker + `</dc:creator></rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`)
}

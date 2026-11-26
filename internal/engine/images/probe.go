package images

import (
	"context"
	"strconv"

	"github.com/angels-you/bee/pkg/types/code"
)

// Info — the header of an image file. Width and Height are as stored;
// Upright swaps them for the EXIF orientations that turn the image.
type Info struct {
	Format      Format
	Width       int
	Height      int
	Bands       int
	Pages       int // frames of an animation, pages of a tiff; only the first is used
	Orientation int // EXIF 1..8, 1 when absent
}

// Upright — width and height after the EXIF orientation is applied.
func (a Info) Upright() (int, int) {
	if a.Orientation >= 5 && a.Orientation <= 8 {
		return a.Height, a.Width
	}
	return a.Width, a.Height
}

// Probe — the header only, nothing is decoded: a file vips cannot
// identify is INVALID_IMAGE, one it reads with a loader of a format gin
// does not accept (svg, pdf, jpeg 2000 …) is UNSUPPORTED_FORMAT.
func (a Tools) Probe(ctx context.Context, file string) (Info, error) {
	h, err := a.header(ctx, file)
	if err != nil {
		if ctx.Err() != nil {
			return Info{}, ctx.Err()
		}
		return Info{}, code.New(code.InvalidImage, "not a readable image", err)
	}
	info := Info{Pages: 1, Orientation: 1}
	switch h["vips-loader"] {
	case "jpegload", "uhdrload":
		info.Format = Jpg
	case "pngload":
		info.Format = Png
	case "webpload":
		info.Format = Webp
	case "gifload":
		info.Format = Gif
	case "tiffload":
		info.Format = Tiff
	case "heifload":
		switch h["heif-compression"] {
		case "av1":
			info.Format = Avif
		case "hevc":
			info.Format = Heic
		default:
			info.Format = Heif
		}
	default:
		return Info{}, code.New(code.UnsupportedFormat, "source format is not jpeg, png, webp, avif, heic, heif, tiff or gif", nil)
	}
	info.Width, _ = strconv.Atoi(h["width"])
	info.Height, _ = strconv.Atoi(h["height"])
	info.Bands, _ = strconv.Atoi(h["bands"])
	if n, err := strconv.Atoi(h["n-pages"]); err == nil && n > 0 {
		info.Pages = n
	}
	if o, err := strconv.Atoi(h["orientation"]); err == nil && o >= 1 && o <= 8 {
		info.Orientation = o
	}
	if info.Width <= 0 || info.Height <= 0 || info.Bands <= 0 {
		return Info{}, code.New(code.InvalidImage, "image header has no size", nil)
	}
	return info, nil
}

// Source — the normalized source every output is made from.
type Source struct {
	File   string
	Width  int
	Height int
	Bands  int // 3, or 4 with alpha
}

// Normalize — the one decode of the source: its first page, turned upright
// by the EXIF orientation, converted to 8-bit sRGB (3 bands, 4 with
// alpha) through its ICC profile, without any metadata, written to dst as
// an uncompressed tiff. Truncated or broken data is INVALID_IMAGE.
func (a Tools) Normalize(ctx context.Context, src string, info Info, dst string) (Source, error) {
	side := strconv.Itoa(max(info.Width, info.Height)) // a box the size of the source: no resize
	_, err := a.run(ctx, a.Vips, "thumbnail", src, dst+"[keep=none]", side, "--height", side,
		"--size", "down", "--output-profile", "srgb", "--fail-on", "error")
	if err != nil {
		if ctx.Err() != nil {
			return Source{}, ctx.Err()
		}
		return Source{}, code.New(code.InvalidImage, "image cannot be decoded", err)
	}
	norm, err := a.Probe(ctx, dst)
	if err != nil {
		return Source{}, code.New(code.InternalTemporaryError, "normalized source unreadable", err)
	}
	return Source{File: dst, Width: norm.Width, Height: norm.Height, Bands: norm.Bands}, nil
}

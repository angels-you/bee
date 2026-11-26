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
	// orientations 5..8 turn the image by 90°
	switch a.Orientation {
	case 5, 6, 7, 8:
		return a.Height, a.Width
	}

	return a.Width, a.Height
}

// Probe — the header only, nothing is decoded: a file vips cannot
// identify is INVALID_IMAGE, one it reads with a loader of a format bee
// does not accept (svg, pdf, jpeg 2000 …) is UNSUPPORTED_FORMAT.
func (a Tools) Probe(ctx context.Context, file string) (Info, error) {
	// header
	h, err := a.header(ctx, file)
	if err != nil {
		if ctx.Err() != nil {
			return Info{}, ctx.Err()
		}

		return Info{}, code.New(code.InvalidImage, "not a readable image", err)
	}

	// format by the loader vips picked
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
		info.Format = a.heifFormat(h["heif-compression"])
	default:
		return Info{}, code.New(code.UnsupportedFormat, "source format is not jpeg, png, webp, avif, heic, heif, tiff or gif", nil)
	}

	// size
	info.Width, _ = strconv.Atoi(h["width"])
	info.Height, _ = strconv.Atoi(h["height"])
	info.Bands, _ = strconv.Atoi(h["bands"])

	// pages, when the header has them
	pages, err := strconv.Atoi(h["n-pages"])
	if err == nil && pages > 0 {
		info.Pages = pages
	}

	// orientation, when the header has a valid one
	orientation, err := strconv.Atoi(h["orientation"])
	if err == nil {
		switch orientation {
		case 1, 2, 3, 4, 5, 6, 7, 8:
			info.Orientation = orientation
		}
	}

	// a picture has a size
	switch {
	case info.Width <= 0:
		return Info{}, code.New(code.InvalidImage, "image header has no size", nil)
	case info.Height <= 0:
		return Info{}, code.New(code.InvalidImage, "image header has no size", nil)
	case info.Bands <= 0:
		return Info{}, code.New(code.InvalidImage, "image header has no size", nil)
	}

	return info, nil
}

// heifFormat — the format of a heif container by its codec.
func (a Tools) heifFormat(compression string) Format {
	switch compression {
	case "av1":
		return Avif
	case "hevc":
		return Heic
	}

	return Heif
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
	// a box the size of the source: no resize
	side := strconv.Itoa(max(info.Width, info.Height))

	// decode; --export-profile: the name of vips 8.16 and older, still
	// accepted by 8.17+ (there --output-profile)
	_, err := a.run(ctx, a.Vips, "thumbnail", src, dst+"[keep=none]", side, "--height", side,
		"--size", "down", "--export-profile", "srgb", "--fail-on", "error")
	if err != nil {
		if ctx.Err() != nil {
			return Source{}, ctx.Err()
		}

		return Source{}, code.New(code.InvalidImage, "image cannot be decoded", err)
	}

	// what was written
	norm, err := a.Probe(ctx, dst)
	if err != nil {
		return Source{}, code.New(code.InternalTemporaryError, "normalized source unreadable", err)
	}

	return Source{File: dst, Width: norm.Width, Height: norm.Height, Bands: norm.Bands}, nil
}

package images

import (
	types "github.com/angels-you/bee/pkg/types/images"
)

// image job types: public, from pkg/types/images
type (
	Profile  = types.Profile
	Format   = types.Format
	Fit      = types.Fit
	Color    = types.Color
	Geometry = types.Geometry
)

// job formats and fits: public, from pkg/types/images
const (
	Avif = types.Avif
	Webp = types.Webp
	Png  = types.Png
	Jpg  = types.Jpg
	Heic = types.Heic
	Heif = types.Heif
	Gif  = types.Gif
	Tiff = types.Tiff

	Inside  = types.Inside
	Cover   = types.Cover
	Contain = types.Contain
	Fill    = types.Fill
)

// blurSigma is the Gaussian sigma of Blur 1 (100 %) as a share of the long side:
// 3 %, 61 px at 2048, 10 px at 320. Shapes blur into color patches
func (a Tools) blurSigma() float64 {
	return 0.03
}

// sharpenSigma is the radius of details Sharpness boosts, as a share of the
// long side: 0.1 %, 2 px at 2048, but no less than sharpenMinSigma
func (a Tools) sharpenSigma() float64 {
	return 0.001
}

// sharpenMinSigma is one pixel: no detail is finer
func (a Tools) sharpenMinSigma() float64 {
	return 0.5
}

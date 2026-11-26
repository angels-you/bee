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

// colorsSide is the box the colours and the thumbhash are measured in:
// a copy of the output that fits in it (thumbhash accepts at most 100×100)
func (a Tools) colorsSide() int {
	return 100
}

// gaussMinAmpl is where vips cuts the gaussian of blur and sharpen: at
// 0.5 % of its peak, ±3.3 sigma. The vips default (20 %, ±1.8 sigma)
// blurs too little and makes a small sigma no blur at all. Blur runs at
// vips "approximate" precision (box filters: a sigma of 180 px at 2048
// takes 0.6 s, exact integer 8 s), within ±1 level of the exact
// gaussian; sharpen at "float" (its sigma is a few px, the integer mask
// of a sub-pixel gaussian is off by several levels)
func (a Tools) gaussMinAmpl() string {
	return "0.005"
}

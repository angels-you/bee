package images

import (
	types "github.com/angels-you/bee/pkg/types/images"
)

// типы job картинок: публичные, из pkg/types/images
type (
	Profile  = types.Profile
	Format   = types.Format
	Fit      = types.Fit
	Color    = types.Color
	Geometry = types.Geometry
)

// форматы и вписывания job: публичные, из pkg/types/images
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

// blurSigma — гауссова сигма Blur 1 (100 %) как доля длинной стороны:
// 3 %, 61 px при 2048, 10 px при 320. Формы расплываются в цветные пятна
func (a Tools) blurSigma() float64 {
	return 0.03
}

// sharpenSigma — радиус деталей, которые усиливает Sharpness, как доля
// длинной стороны: 0.1 %, 2 px при 2048, но не меньше sharpenMinSigma
func (a Tools) sharpenSigma() float64 {
	return 0.001
}

// sharpenMinSigma — пиксель: мельче деталей не бывает
func (a Tools) sharpenMinSigma() float64 {
	return 0.5
}

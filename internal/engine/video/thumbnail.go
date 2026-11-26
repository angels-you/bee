package video

import (
	"math"
	"strconv"

	job "github.com/angels-you/bee/pkg/types/video"
)

// stillArgs — the encoder and muxer of a thumbnail; Quality 1..100 is
// mapped onto the scale of each encoder:
//
//	jpg   mjpeg -q:v 31..2 (lower is better)
//	png   lossless, Quality is not used
//	webp  libwebp -quality 1..100
//	avif  libsvtav1 -crf 62..0 (lower is better), the avif muxer
func (a *Tools) stillArgs(s Still) []string {
	q := float64(s.Thumbnail.Quality)

	switch s.Thumbnail.Format {
	case job.ImageJPEG:
		scale := 2 + int(math.Round((100-q)*29/99))
		return []string{"-c:v", "mjpeg", "-q:v", strconv.Itoa(scale), "-f", "image2", "-update", "1"}
	case job.ImagePNG:
		return []string{"-c:v", "png", "-f", "image2", "-update", "1"}
	case job.ImageWebP:
		return []string{"-c:v", "libwebp", "-quality", strconv.Itoa(s.Thumbnail.Quality), "-f", "image2", "-update", "1"}
	}

	// avif
	crf := int(math.Round((100 - q) * 63 / 100))
	return []string{"-c:v", "libsvtav1", "-crf", strconv.Itoa(crf), "-f", "avif"}
}

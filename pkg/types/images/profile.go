package images

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/angels-you/bee/pkg/types/code"
)

// Profile — как сделать один результат из оригинала. Пустые параметры
// обработки (Fit, Effort, MinQuality) берутся из дефолтов bee (env).
// Brightness, Sharpness и Blur — множители в стиле процентов: 1 = 100 %,
// 1.5 = 150 %, 0.9 = 90 %; их радиусы — доли длинной стороны результата,
// поэтому эффект выглядит одинаково на любом размере
type Profile struct {
	Format Format `json:"format"`        // результат: "jpg", "png", "webp" или "avif"
	Fit    Fit    `json:"fit,omitempty"` // "inside", "cover", "contain" или "fill", см. Geometry; пусто — inside

	// Quality 1..100 для jpg и lossy webp / avif; для png и lossless не
	// нужен (может быть 0)
	Quality   int  `json:"quality"`
	MaxWidth  int  `json:"max_width"`  // ширина коробки, px; 0 — без ограничения (только inside)
	MaxHeight int  `json:"max_height"` // высота коробки, px; 0 — без ограничения (только inside)
	Center    bool `json:"center"`     // обрезка cover: true — центр, false — самая интересная область (vips attention)

	// BackgroundColor — #rgb, #rrggbb или #rrggbbaa. Фон contain и то, на
	// что jpg кладёт прозрачность. Пусто: contain — прозрачный фон (чёрный
	// в jpg), jpg — на чёрный
	BackgroundColor string `json:"background_color"`

	// Blur — сила гауссова размытия: 0 — нет, 1 = 100 %, 0.5 = 50 %; до
	// MaxBlur
	Blur float64 `json:"blur"`

	// Brightness — множитель цветовых каналов, альфа не меняется: 1 — без
	// изменений, 1.5 = 150 %; 0 (не задано) — без изменений; до
	// MaxBrightness
	Brightness float64 `json:"brightness"`

	// Sharpness — unsharp mask: 1 — без изменений, 1.5 — мелкие детали на
	// 150 % (резче), 0.9 — на 90 % (мягче); 0 (не задано) — без изменений;
	// до MaxSharpness
	Sharpness float64 `json:"sharpness"`

	Upscale       bool `json:"upscale"`        // оригинал можно увеличить до коробки
	MaxBytes      int  `json:"max_bytes"`      // лимит размера результата, байт, 0 — нет; lossy качество снижается до MinQuality
	Thumbhash     bool `json:"thumbhash"`      // TaskResult.Thumbhash результата
	Lossless      bool `json:"lossless"`       // lossless webp / avif; png всегда lossless, jpg никогда
	DominantColor bool `json:"dominant_color"` // TaskResult.DominantColor результата
	PaletteColors int  `json:"palette_colors"` // TaskResult.Palette результата, 0..MaxPalette цветов
	AverageColor  bool `json:"average_color"`  // TaskResult.AverageColor результата

	// MinQuality — ниже этого качества max_bytes не опускается, 1..100; 0 —
	// BEE_IMAGES_MIN_QUALITY
	MinQuality int `json:"min_quality,omitempty"`

	// Effort — усилие кодера avif, 0..9: выше — медленнее и меньше; nil —
	// BEE_IMAGES_AVIF_EFFORT (0 — осмысленное значение, поэтому указатель)
	Effort *int `json:"effort,omitempty"`
}

// Color — прямой (не premultiplied) rgba
type Color struct {
	R, G, B, A uint8
}

// Hex — #rrggbb, альфа отброшена
func (a Color) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", a.R, a.G, a.B)
}

// MaxBrightness — предел Brightness: 300 %, каналы × 3
func (a Profile) MaxBrightness() float64 {
	return 3
}

// MaxSharpness — предел Sharpness: 300 %, мелкие детали × 3
func (a Profile) MaxSharpness() float64 {
	return 3
}

// MaxBlur — предел Blur: 300 %, три сигмы Blur 1
func (a Profile) MaxBlur() float64 {
	return 3
}

// MaxPalette — предел PaletteColors, цветов
func (a Profile) MaxPalette() int {
	return 16
}

// FitOrInside — Fit, пустой — inside
func (a Profile) FitOrInside() Fit {
	if a.Fit == "" {
		return Inside
	}

	return a.Fit
}

// Validate — все поля в своих пределах. Лимиты bee (самая длинная
// сторона) проверяет движок
func (a Profile) Validate() *code.Error {
	fit := a.FitOrInside()

	// формат и вписывание
	switch {
	case !a.Format.IsOutput():
		return code.New(code.InvalidJob, fmt.Sprintf("format %q is not jpg, png, webp or avif", string(a.Format)), nil)
	case !fit.Valid():
		return code.New(code.InvalidJob, fmt.Sprintf("fit %q is not inside, cover, contain or fill", string(a.Fit)), nil)
	case a.Lossless && a.Format == Jpg:
		return code.New(code.InvalidJob, "jpg has no lossless mode", nil)
	}

	// качество
	switch {
	case a.Quality < 0, a.Quality > 100:
		return code.New(code.InvalidQuality, "quality must be within 0..100", nil)
	case a.Quality == 0 && a.Lossy():
		return code.New(code.InvalidQuality, "quality is required for a lossy "+a.Format.String(), nil)
	case a.MinQuality < 0, a.MinQuality > 100:
		return code.New(code.InvalidQuality, "min_quality must be within 0..100", nil)
	}

	// коробка
	switch {
	case a.MaxWidth < 0, a.MaxHeight < 0:
		return code.New(code.InvalidDimensions, "max_width and max_height must not be negative", nil)
	case fit != Inside && (a.MaxWidth == 0 || a.MaxHeight == 0):
		return code.New(code.InvalidDimensions, fit.String()+" needs max_width and max_height", nil)
	}

	// эффекты: !(x >= 0 && x <= max) ловит и NaN
	switch {
	case !(a.Blur >= 0 && a.Blur <= a.MaxBlur()):
		return code.New(code.InvalidJob, fmt.Sprintf("blur must be within 0..%v", a.MaxBlur()), nil)
	case !(a.Sharpness >= 0 && a.Sharpness <= a.MaxSharpness()):
		return code.New(code.InvalidJob, fmt.Sprintf("sharpness must be within 0..%v", a.MaxSharpness()), nil)
	case !(a.Brightness >= 0 && a.Brightness <= a.MaxBrightness()):
		return code.New(code.InvalidJob, fmt.Sprintf("brightness must be within 0..%v", a.MaxBrightness()), nil)
	}

	// остальное
	switch {
	case a.MaxBytes < 0:
		return code.New(code.InvalidJob, "max_bytes must not be negative", nil)
	case a.PaletteColors < 0, a.PaletteColors > a.MaxPalette():
		return code.New(code.InvalidJob, fmt.Sprintf("palette_colors must be within 0..%d", a.MaxPalette()), nil)
	case a.Effort != nil && (*a.Effort < 0 || *a.Effort > 9):
		return code.New(code.InvalidJob, "effort must be within 0..9", nil)
	}

	// цвет фона
	_, err := a.Background()
	if err != nil {
		return code.New(code.InvalidJob, err.Error(), nil)
	}

	return nil
}

// Background — BackgroundColor: #rgb, #rrggbb или #rrggbbaa, альфа 255,
// если не задана; пусто — прозрачный
func (a Profile) Background() (Color, error) {
	s := a.BackgroundColor
	if s == "" {
		return Color{}, nil
	}

	// без решётки — не цвет
	hexa, ok := strings.CutPrefix(s, "#")
	if !ok {
		return Color{}, fmt.Errorf("background_color %q is not #rgb, #rrggbb or #rrggbbaa", s)
	}

	// #rgb → #rrggbb
	if len(hexa) == 3 {
		hexa = string([]byte{hexa[0], hexa[0], hexa[1], hexa[1], hexa[2], hexa[2]})
	}

	// #rrggbb → #rrggbbff
	if len(hexa) == 6 {
		hexa += "ff"
	}

	b, err := hex.DecodeString(hexa)
	if err != nil || len(b) != 4 {
		return Color{}, fmt.Errorf("background_color %q is not #rgb, #rrggbb or #rrggbbaa", s)
	}

	return Color{R: b[0], G: b[1], B: b[2], A: b[3]}, nil
}

// Lossy — кодирование берёт Quality: jpg, и webp / avif без lossless
func (a Profile) Lossy() bool {
	switch {
	case a.Format == Jpg:
		return true
	case a.Format == Png:
		return false
	}

	return !a.Lossless
}

// Geometry — размер результата для оригинала w×h (уже повёрнутого).
//
//	inside   вписан в коробку, пропорции сохранены; ось 0 без ограничения,
//	         без коробки — размер оригинала
//	cover    ровно коробка: покрыть её, лишнее обрезать; без Upscale
//	         оригинал меньше коробки не увеличивается: результат — самая
//	         большая обрезка с пропорциями коробки
//	contain  ровно коробка: картинка вписана (как inside), по центру,
//	         остальное залито BackgroundColor
//	fill     ровно коробка, растянуто; без Upscale каждая сторона не
//	         больше стороны оригинала
//
// Без Upscale inside и contain оригинал не увеличивают
func (a Profile) Geometry(w, h int) Geometry {
	bw := float64(a.MaxWidth)
	bh := float64(a.MaxHeight)
	fw := float64(w)
	fh := float64(h)

	switch a.FitOrInside() {
	case Cover:
		return a.cover(bw, bh, fw, fh)
	case Fill:
		return a.fill(w, h)
	}

	// масштаб inside и contain
	s := math.Inf(1)
	if a.MaxWidth > 0 {
		s = bw / fw
	}
	if a.MaxHeight > 0 {
		s = math.Min(s, bh/fh)
	}

	switch {
	case math.IsInf(s, 1):
		s = 1
	case !a.Upscale && s > 1:
		s = 1
	}

	iw := max(1, int(math.Round(fw*s)))
	ih := max(1, int(math.Round(fh*s)))

	if a.Fit == Contain {
		return Geometry{Width: a.MaxWidth, Height: a.MaxHeight, ImageWidth: iw, ImageHeight: ih}
	}

	return Geometry{Width: iw, Height: ih, ImageWidth: iw, ImageHeight: ih}
}

// cover — коробка целиком или, без Upscale, самая большая обрезка её
// пропорций в оригинале
func (a Profile) cover(bw, bh, fw, fh float64) Geometry {
	if a.Upscale || (bw <= fw && bh <= fh) {
		return Geometry{Width: a.MaxWidth, Height: a.MaxHeight, ImageWidth: a.MaxWidth, ImageHeight: a.MaxHeight}
	}

	// меньше 1: коробка не помещается в оригинал
	k := math.Min(fw/bw, fh/bh)
	cw := max(1, int(math.Round(bw*k)))
	ch := max(1, int(math.Round(bh*k)))

	return Geometry{Width: cw, Height: ch, ImageWidth: cw, ImageHeight: ch}
}

// fill — коробка, без Upscale не больше оригинала по каждой стороне
func (a Profile) fill(w, h int) Geometry {
	if a.Upscale {
		return Geometry{Width: a.MaxWidth, Height: a.MaxHeight, ImageWidth: a.MaxWidth, ImageHeight: a.MaxHeight}
	}

	sw := min(a.MaxWidth, w)
	sh := min(a.MaxHeight, h)

	return Geometry{Width: sw, Height: sh, ImageWidth: sw, ImageHeight: sh}
}

// Geometry — размер одного результата: результат Width×Height, картинка в
// нём ImageWidth×ImageHeight (меньше только у contain, остальное — фон)
type Geometry struct {
	Width, Height           int
	ImageWidth, ImageHeight int
}

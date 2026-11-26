package config

import (
	"errors"
	"time"
)

// Images — лимиты картинок и дефолты для пустых полей images.Profile
type Images struct {

	// лимиты сервера: job их не меняет
	Timeout       time.Duration `env:"TIMEOUT"`        // одна job, от скачивания до загрузки
	MaxSourceMB   int64         `env:"MAX_SOURCE_MB"`  // самый большой оригинал, MiB
	MaxMegapixels int           `env:"MAX_MEGAPIXELS"` // ширина × высота оригинала, миллионов
	MaxDimension  int           `env:"MAX_DIMENSION"`  // самая длинная сторона результата (и коробки задачи), px
	Threads       int           `env:"THREADS"`        // потоков одного запуска vips

	// дефолты обработки: пустое поле job = это значение
	MinQuality int `env:"MIN_QUALITY"` // max_bytes не опускает качество ниже, 1..100
	AvifEffort int `env:"AVIF_EFFORT"` // усилие кодера avif, 0..9: выше — медленнее и меньше
}

// defaults — значения из рабочего gin
func (a *Images) defaults() {
	a.Timeout = 2 * time.Minute
	a.MaxSourceMB = 50
	a.MaxMegapixels = 100
	a.MaxDimension = 8000
	a.Threads = 4

	a.MinQuality = 30
	a.AvifEffort = 4
}

// validate — все поля в своих пределах
func (a *Images) validate(download time.Duration) (errs []error) {

	// лимиты
	if a.Timeout <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_TIMEOUT must be positive"))
	}

	if download > a.Timeout {
		errs = append(errs, errors.New("config: BEE_DOWNLOAD_TIMEOUT must be at most BEE_IMAGES_TIMEOUT"))
	}

	if a.MaxSourceMB <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_MAX_SOURCE_MB must be positive"))
	}

	if a.MaxMegapixels <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_MAX_MEGAPIXELS must be positive"))
	}

	if a.MaxDimension <= 0 {
		errs = append(errs, errors.New("config: BEE_IMAGES_MAX_DIMENSION must be positive"))
	}

	if a.Threads < 1 {
		errs = append(errs, errors.New("config: BEE_IMAGES_THREADS must be at least 1"))
	}

	// дефолты обработки
	if !(a.MinQuality >= 1 && a.MinQuality <= 100) {
		errs = append(errs, errors.New("config: BEE_IMAGES_MIN_QUALITY must be within 1..100"))
	}

	if !(a.AvifEffort >= 0 && a.AvifEffort <= 9) {
		errs = append(errs, errors.New("config: BEE_IMAGES_AVIF_EFFORT must be within 0..9"))
	}

	return
}

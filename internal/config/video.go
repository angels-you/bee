package config

import (
	"errors"
	"time"
)

// Video — лимиты видео job и дефолты для пустых полей video job
type Video struct {

	// лимиты сервера: job их не меняет
	Timeout      time.Duration `env:"TIMEOUT"`       // одна job, от скачивания до загрузки
	MaxSourceMB  int64         `env:"MAX_SOURCE_MB"` // каждый скачанный файл (видео, звук, водяной знак), MiB
	MinDuration  time.Duration `env:"MIN_DURATION"`  // самый короткий оригинал и результат
	MaxDuration  time.Duration `env:"MAX_DURATION"`  // самый длинный оригинал и результат
	MaxDimension int           `env:"MAX_DIMENSION"` // самая длинная сторона кадра оригинала и результата, px
	Threads      int           `env:"THREADS"`       // потоков одного запуска ffmpeg

	// дефолты обработки: пустое поле job = это значение
	SoundBitrate   int     `env:"SOUND_BITRATE"`   // AAC kbit/s профиля с sound_bitrate 0
	WatermarkWidth int     `env:"WATERMARK_WIDTH"` // ширина водяного знака с width 0, px
	ImageQuality   int     `env:"IMAGE_QUALITY"`   // качество превью-картинки с quality 0, 1..100
	LUFS           float64 `env:"LUFS"`            // цель громкости options.normalize, -70..-5 LUFS
	TruePeak       float64 `env:"TRUE_PEAK"`       // потолок true peak options.normalize, -9..0 dBTP
}

// defaults — значения из рабочего reels
func (a *Video) defaults() {
	a.Timeout = 10 * time.Minute
	a.MaxSourceMB = 500
	a.MinDuration = time.Second
	a.MaxDuration = 5 * time.Minute
	a.MaxDimension = 3840
	a.Threads = 4

	a.SoundBitrate = 128
	a.WatermarkWidth = 160
	a.ImageQuality = 80
	a.LUFS = -16
	a.TruePeak = -1.5
}

// validate — все поля в своих пределах
func (a *Video) validate(download time.Duration) (errs []error) {

	// лимиты
	if a.Timeout <= 0 {
		errs = append(errs, errors.New("config: BEE_VIDEO_TIMEOUT must be positive"))
	}

	if download > a.Timeout {
		errs = append(errs, errors.New("config: BEE_DOWNLOAD_TIMEOUT must be at most BEE_VIDEO_TIMEOUT"))
	}

	if a.MaxSourceMB <= 0 {
		errs = append(errs, errors.New("config: BEE_VIDEO_MAX_SOURCE_MB must be positive"))
	}

	if a.MinDuration <= 0 {
		errs = append(errs, errors.New("config: BEE_VIDEO_MIN_DURATION must be positive"))
	}

	if a.MaxDuration <= a.MinDuration {
		errs = append(errs, errors.New("config: BEE_VIDEO_MAX_DURATION must be above BEE_VIDEO_MIN_DURATION"))
	}

	if a.MaxDimension < 2 {
		errs = append(errs, errors.New("config: BEE_VIDEO_MAX_DIMENSION must be at least 2"))
	}

	if a.Threads < 1 {
		errs = append(errs, errors.New("config: BEE_VIDEO_THREADS must be at least 1"))
	}

	// дефолты обработки
	if !(a.SoundBitrate >= 16 && a.SoundBitrate <= 512) {
		errs = append(errs, errors.New("config: BEE_VIDEO_SOUND_BITRATE must be within 16..512 kbit/s"))
	}

	if !(a.WatermarkWidth >= 1 && a.WatermarkWidth <= a.MaxDimension) {
		errs = append(errs, errors.New("config: BEE_VIDEO_WATERMARK_WIDTH must be within 1..BEE_VIDEO_MAX_DIMENSION"))
	}

	if !(a.ImageQuality >= 1 && a.ImageQuality <= 100) {
		errs = append(errs, errors.New("config: BEE_VIDEO_IMAGE_QUALITY must be within 1..100"))
	}

	if !(a.LUFS >= -70 && a.LUFS <= -5) {
		errs = append(errs, errors.New("config: BEE_VIDEO_LUFS must be within -70..-5"))
	}

	if !(a.TruePeak >= -9 && a.TruePeak <= 0) {
		errs = append(errs, errors.New("config: BEE_VIDEO_TRUE_PEAK must be within -9..0 dBTP"))
	}

	return
}

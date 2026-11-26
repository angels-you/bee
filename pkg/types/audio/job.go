// Package audio — job аудио bee и её результат, одинаковые на любом
// транспорте (http, grpc, nats). Без зависимостей от сервера: пакет
// импортирует клиент
package audio

import (
	"path"
	"strings"

	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/code"
)

// Job — один трек. Input — url оригинала (presigned GET или любой
// публичный https), в логах без query.
//
// Ключи результатов (Output, Wave, Meta, Analysis) выбирает клиент — это
// ключи в хранилище bee (не бакет и не endpoint). Output обязателен,
// остальные нет: заданный ключ — результат публикуется туда, пустой — не
// делается. Если по Output уже лежит master playlist, job отклоняется:
// повторный запуск — новые ключи.
//
// Key — необязательный AES-128 ключ трека: с ним каждый сегмент HLS
// шифруется, без него HLS публикуется открытым; никогда не пишется в
// логи и не сохраняется
type Job struct {
	ID    string `json:"id"`
	Input string `json:"input"` // url оригинала

	// Output — ключ master playlist (.m3u8); каждый вариант лежит в папке
	// с именем его битрейта в kbit/s рядом с ним:
	// <папка output>/<kbit>/index.m3u8, init.mp4, seg_NNNN.m4s
	Output string `json:"output"`

	Wave     string `json:"wave,omitempty"`     // ключ waveform json; пусто — без waveform (и без Result.Waveform)
	Meta     string `json:"meta,omitempty"`     // ключ json метаданных; пусто — не публикуется
	Analysis string `json:"analysis,omitempty"` // ключ json анализа; пусто — не публикуется

	Key    []byte `json:"key,omitempty"`     // пусто (открытый HLS) или ровно 16 байт; base64 в json
	KeyURI string `json:"key_uri,omitempty"` // обязателен с Key: адрес ключа для плеера, например skd://1001

	Options Options `json:"options,omitempty"`
}

// Options — параметры обработки. Пустое поле — дефолт bee (env
// BEE_AUDIO_*)
type Options struct {
	Normalize bool `json:"normalize,omitempty"` // выровнять громкость до LUFS, не выше TruePeak

	LUFS     float64 `json:"lufs,omitempty"`      // цель громкости, ниже 0 и выше -70 LUFS; 0 — BEE_AUDIO_LUFS
	TruePeak float64 `json:"true_peak,omitempty"` // потолок true peak, ниже 0 dBTP; 0 — BEE_AUDIO_TRUE_PEAK

	// Renditions — битрейты AAC вариантов HLS, kbit/s, разные и
	// положительные, в порядке master playlist: плеер начинает с первого.
	// Пусто — BEE_AUDIO_RENDITIONS
	Renditions []int `json:"renditions,omitempty"`

	SampleRate int `json:"sample_rate,omitempty"` // Hz каждого варианта; 0 — BEE_AUDIO_SAMPLE_RATE
	Channels   int `json:"channels,omitempty"`    // каналов каждого варианта; 0 — BEE_AUDIO_CHANNELS
	Segment    int `json:"segment,omitempty"`     // длина сегмента HLS, секунд; 0 — BEE_AUDIO_SEGMENT

	WaveformPPS    int `json:"waveform_pps,omitempty"`    // точек в секунду в waveform json; 0 — BEE_AUDIO_WAVEFORM_PPS
	WaveformPoints int `json:"waveform_points,omitempty"` // точек в Result.Waveform; 0 — BEE_AUDIO_WAVEFORM_POINTS

	// флаги: nil — дефолт bee, false и true — осмысленные значения
	Analysis    *bool `json:"analysis,omitempty"`    // диагностика качества (Result.Quality); nil — BEE_AUDIO_ANALYSIS
	Fingerprint *bool `json:"fingerprint,omitempty"` // chromaprint (Result.Fingerprint); nil — BEE_AUDIO_FINGERPRINT
}

// Validate — job без учёта настроек bee: id, url, ключи, ключ шифрования
// и параметры. Ключи результатов приводятся к чистому виду на месте.
// Схему и хост url, лимиты и папки вариантов проверяет движок
func (a *Job) Validate() *code.Error {
	if a.ID == "" {
		return code.New(code.InvalidJob, "id is required", nil)
	}

	// url оригинала
	_, ok := tools.URL(a.Input)
	if !ok {
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	}

	// ключи результатов
	e := a.validateKeys()
	if e != nil {
		return e
	}

	// ключ шифрования
	e = a.validateKey()
	if e != nil {
		return e
	}

	return a.Options.Validate()
}

// validateKeys — Output обязателен и .m3u8, остальные ключи по желанию;
// все чистые и разные
func (a *Job) validateKeys() *code.Error {
	taken := map[string]string{}

	// master playlist обязателен
	if a.Output == "" {
		return code.New(code.InvalidPath, "output is required", nil)
	}

	e := a.cleanKey("output", &a.Output, taken)
	if e != nil {
		return e
	}

	if path.Ext(a.Output) != ".m3u8" {
		return code.New(code.InvalidPath, "output must end in .m3u8", nil)
	}

	// waveform
	e = a.cleanKey("wave", &a.Wave, taken)
	if e != nil {
		return e
	}

	// метаданные
	e = a.cleanKey("meta", &a.Meta, taken)
	if e != nil {
		return e
	}

	// анализ
	return a.cleanKey("analysis", &a.Analysis, taken)
}

// cleanKey — ключ name чистый и не занят другим; пустой пропускается.
// taken — уже занятые ключи и их имена
func (a *Job) cleanKey(name string, raw *string, taken map[string]string) *code.Error {
	if *raw == "" {
		return nil
	}

	key, ok := tools.Key(*raw)
	switch {
	case !ok:
		return code.New(code.InvalidPath, name+" must be a clean relative storage key", nil)
	case taken[key] != "":
		return code.New(code.InvalidPath, name+" is the same key as "+taken[key], nil)
	}

	taken[key] = name
	*raw = key

	return nil
}

// validateKey — ключ шифрования: пусто или 16 байт, с ним нужен KeyURI
func (a *Job) validateKey() *code.Error {
	if len(a.Key) == 0 {
		return nil
	}

	switch {
	case len(a.Key) != 16:
		return code.New(code.InvalidJob, "key must be 16 bytes", nil)
	case a.KeyURI == "":
		return code.New(code.InvalidJob, "key_uri is required with a key", nil)
	case strings.ContainsAny(a.KeyURI, "\"\r\n"):
		return code.New(code.InvalidJob, "key_uri holds a quote or a line break", nil)
	}

	return nil
}

// Validate — параметры в своих пределах; пустые не проверяются: это
// дефолты bee
func (a *Options) Validate() *code.Error {

	// громкость
	switch {
	case a.LUFS > 0:
		return code.New(code.InvalidJob, "lufs must be below 0", nil)
	case a.LUFS != 0 && a.LUFS <= -70:
		return code.New(code.InvalidJob, "lufs must be above -70", nil)
	case a.TruePeak > 0:
		return code.New(code.InvalidJob, "true_peak must be below 0", nil)
	}

	// варианты: папка варианта — его битрейт, поэтому разные и положительные
	seen := map[int]bool{}
	for _, kbit := range a.Renditions {
		switch {
		case kbit <= 0:
			return code.New(code.InvalidJob, "renditions must be positive kbit/s", nil)
		case seen[kbit]:
			return code.New(code.InvalidJob, "renditions must be distinct", nil)
		}
		seen[kbit] = true
	}

	// формат и waveform
	switch {
	case a.SampleRate < 0:
		return code.New(code.InvalidJob, "sample_rate must not be negative", nil)
	case a.Channels < 0:
		return code.New(code.InvalidJob, "channels must not be negative", nil)
	case a.Segment < 0:
		return code.New(code.InvalidJob, "segment must not be negative", nil)
	case a.WaveformPPS < 0:
		return code.New(code.InvalidJob, "waveform_pps must not be negative", nil)
	case a.WaveformPoints < 0:
		return code.New(code.InvalidJob, "waveform_points must not be negative", nil)
	}

	return nil
}

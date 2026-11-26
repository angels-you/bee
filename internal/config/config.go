// Package config — настройки bee из env (envy, префикс BEE_). Обязательны
// только хранилище (BEE_STORAGE и ключи выбранного вида) и хотя бы один
// транспорт (BEE_HTTP_ADDR, BEE_GRPC_ADDR или BEE_NATS_URL); у всего
// остального есть дефолты (defaults), все они видны в cmd/.env.
//
// Лимиты сервера (размер, длительность, concurrency, timeout) job не
// меняет; параметры обработки здесь — только дефолты для пустых полей job
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monopolly/envy"
)

// Config — все настройки bee
type Config struct {
	WorkerID          string        `env:"WORKER_ID"`             // имя bee в логах и в nats
	TempDir           string        `env:"TEMP_DIR"`              // здесь создаются временные папки job
	Concurrency       int           `env:"CONCURRENCY"`           // job одновременно, все типы вместе, не меньше 1
	QueueWait         time.Duration `env:"QUEUE_WAIT"`            // сколько job ждёт свободный слот, 0 — сразу отказ
	DownloadTimeout   time.Duration `env:"DOWNLOAD_TIMEOUT"`      // скачивание всех url одной job, не больше Timeout типа
	UploadConcurrency int           `env:"UPLOAD_CONCURRENCY"`    // объектов одной job загружается одновременно
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT"`      // сколько работающие job получают при остановке
	Token             string        `env:"TOKEN"`                 // токен http и grpc; пусто — без проверки
	SourceHosts       []string      `env:"SOURCE_HOSTS"`          // разрешённые хосты url (по суффиксу); пусто — любой публичный
	AllowInsecure     bool          `env:"ALLOW_INSECURE_SOURCE"` // http и приватные адреса: только разработка и тесты

	Tools   Tools
	Storage Storage
	Audio   Audio  `prefix:"AUDIO_"`
	Video   Video  `prefix:"VIDEO_"`
	Images  Images `prefix:"IMAGES_"`
	HTTP    HTTP   `prefix:"HTTP_"`
	GRPC    GRPC   `prefix:"GRPC_"`
	NATS    NATS   `prefix:"NATS_"`
}

// Load — дефолты, потом .env файлы (если переданы; настоящий env важнее
// файла), потом env BEE_*, и проверка. Ошибка называет каждое неверное
// поле
func Load(files ...string) (*Config, error) {
	a := &Config{}
	a.defaults()

	// .env файлы
	for _, name := range files {
		err := envy.File(name)
		if err != nil {
			return nil, err
		}
	}

	// env поверх дефолтов: незаданный ключ дефолт не трогает
	env := struct {
		Bee *Config `prefix:"BEE_"`
	}{Bee: a}

	err := envy.Parse(&env)
	if err != nil {
		return nil, err
	}

	err = a.validate()
	if err != nil {
		return nil, err
	}

	return a, nil
}

// defaults — значения всех необязательных ключей; их же показывает
// cmd/.env. Хранилище и транспорты дефолтов не имеют
func (a *Config) defaults() {
	a.WorkerID = "bee-1"
	a.TempDir = "/tmp/bee"
	a.Concurrency = 2
	a.QueueWait = 0
	a.DownloadTimeout = 2 * time.Minute
	a.UploadConcurrency = 8
	a.ShutdownTimeout = 2 * time.Minute
	a.Token = ""
	a.SourceHosts = nil
	a.AllowInsecure = false

	a.Tools.defaults()
	a.Storage.defaults()
	a.Audio.defaults()
	a.Video.defaults()
	a.Images.defaults()
	a.NATS.defaults()
}

// validate — все поля в своих пределах; все ошибки сразу, по строке на
// поле
func (a *Config) validate() error {
	var errs []error

	errs = append(errs, a.validateCommon()...)
	errs = append(errs, a.Tools.validate(a.Audio.Fingerprint)...)
	errs = append(errs, a.Storage.validate()...)
	errs = append(errs, a.Audio.validate(a.DownloadTimeout)...)
	errs = append(errs, a.Video.validate(a.DownloadTimeout)...)
	errs = append(errs, a.Images.validate(a.DownloadTimeout)...)
	errs = append(errs, a.validateTransports()...)

	return errors.Join(errs...)
}

// validateCommon — общие поля
func (a *Config) validateCommon() (errs []error) {
	if a.WorkerID == "" {
		errs = append(errs, errors.New("config: BEE_WORKER_ID is required"))
	}

	if a.TempDir == "" {
		errs = append(errs, errors.New("config: BEE_TEMP_DIR is required"))
	}

	if a.Concurrency < 1 {
		errs = append(errs, errors.New("config: BEE_CONCURRENCY must be at least 1"))
	}

	if a.QueueWait < 0 {
		errs = append(errs, errors.New("config: BEE_QUEUE_WAIT must not be negative"))
	}

	if a.DownloadTimeout <= 0 {
		errs = append(errs, errors.New("config: BEE_DOWNLOAD_TIMEOUT must be positive"))
	}

	if a.UploadConcurrency < 1 {
		errs = append(errs, errors.New("config: BEE_UPLOAD_CONCURRENCY must be at least 1"))
	}

	if a.ShutdownTimeout < 0 {
		errs = append(errs, errors.New("config: BEE_SHUTDOWN_TIMEOUT must not be negative"))
	}

	// хосты: без пустых и пробелов
	for _, h := range a.SourceHosts {
		switch {
		case h == "":
			errs = append(errs, errors.New("config: BEE_SOURCE_HOSTS: empty host"))
		case h != strings.TrimSpace(h):
			errs = append(errs, fmt.Errorf("config: BEE_SOURCE_HOSTS: %q is not a host", h))
		}
	}

	return
}

// validateTransports — хотя бы один транспорт, и включённый nats в
// своих пределах
func (a *Config) validateTransports() (errs []error) {
	if a.transports() == 0 {
		errs = append(errs, errors.New("config: no transport: set BEE_HTTP_ADDR, BEE_GRPC_ADDR or BEE_NATS_URL"))
	}

	if a.NATS.Enabled() {
		errs = append(errs, a.NATS.validate()...)
	}

	return
}

// SourceHostAllowed — host один из SourceHosts или под ним; любой, если
// список пуст. Для storage.Downloader.HostAllowed
func (a *Config) SourceHostAllowed(host string) bool {
	if len(a.SourceHosts) == 0 {
		return true
	}

	host = strings.ToLower(host)
	for _, h := range a.SourceHosts {
		h = strings.ToLower(h)

		switch {
		case host == h:
			return true
		case strings.HasSuffix(host, "."+h):
			return true
		}
	}

	return false
}

// transports — сколько транспортов включено
func (a *Config) transports() (n int) {
	if a.HTTP.Enabled() {
		n++
	}

	if a.GRPC.Enabled() {
		n++
	}

	if a.NATS.Enabled() {
		n++
	}

	return
}

// Defaults — конфиг только с дефолтами, без env и без проверки: основа
// для тестов, хранилище и транспорты не заданы
func Defaults() *Config {
	a := &Config{}
	a.defaults()

	return a
}

package config

import (
	"errors"
	"fmt"

	"github.com/angels-you/bee/pkg/storage"
	"github.com/angels-you/bee/pkg/storage/local"
	"github.com/angels-you/bee/pkg/storage/s3"
	"github.com/angels-you/bee/pkg/storage/s3/provider"
)

// Storage — единственное хранилище bee: Kind выбирает local или s3, и
// обязательны только ключи выбранного вида
type Storage struct {
	Kind  string     `env:"STORAGE"` // s3 или local, обязательно
	Local LocalStore `prefix:"LOCAL_"`
	S3    S3Store    `prefix:"S3_"`
}

// LocalStore — папка на диске
type LocalStore struct {
	Dir string `env:"DIR"` // корень, обязательно для local
}

// S3Store — один бакет любого s3 провайдера. Пустые Endpoint, Region,
// Account и Prefix имеют смысл: endpoint и регион провайдера, без
// аккаунта, без префикса
type S3Store struct {
	Provider string `env:"PROVIDER"` // cloudflare, hetzner, aws, minio, ... (pkg/storage/s3/provider), обязательно
	Endpoint string `env:"ENDPOINT"` // пусто — по шаблону провайдера
	Region   string `env:"REGION"`   // пусто — регион провайдера
	Account  string `env:"ACCOUNT"`  // cloudflare account id и т.п.
	Key      string `env:"KEY"`      // обязательно
	Secret   string `env:"SECRET"`   // обязательно
	Bucket   string `env:"BUCKET"`   // обязательно
	Prefix   string `env:"PREFIX"`   // папка внутри бакета; пусто — корень
	Secure   bool   `env:"SECURE"`   // https; учитывается только у своих (minio, ceph), облачные всегда https
}

// defaults — у хранилища нет дефолтов, кроме https
func (a *Storage) defaults() {
	a.S3.Secure = true
}

// validate — вид известен и ключи этого вида заданы
func (a *Storage) validate() (errs []error) {
	switch a.Kind {
	case "local":
		if a.Local.Dir == "" {
			errs = append(errs, errors.New("config: BEE_LOCAL_DIR is required with BEE_STORAGE=local"))
		}
	case "s3":
		errs = append(errs, a.S3.validate()...)
	case "":
		errs = append(errs, errors.New("config: BEE_STORAGE is required: s3 or local"))
	default:
		errs = append(errs, fmt.Errorf("config: BEE_STORAGE %q is not s3 or local", a.Kind))
	}

	return
}

// validate — обязательные ключи бакета и известный провайдер
func (a *S3Store) validate() (errs []error) {
	if !a.provider().Valid() {
		errs = append(errs, fmt.Errorf("config: BEE_S3_PROVIDER %q is not known", a.Provider))
	}

	if a.Key == "" {
		errs = append(errs, errors.New("config: BEE_S3_KEY is required with BEE_STORAGE=s3"))
	}

	if a.Secret == "" {
		errs = append(errs, errors.New("config: BEE_S3_SECRET is required with BEE_STORAGE=s3"))
	}

	if a.Bucket == "" {
		errs = append(errs, errors.New("config: BEE_S3_BUCKET is required with BEE_STORAGE=s3"))
	}

	return
}

// provider — провайдер по имени, 0 если неизвестен
func (a *S3Store) provider() provider.Provider {
	return provider.Provider(provider.ProviderIndex(a.Provider))
}

// Open — хранилище выбранного вида; бакет s3 проверяется Ping
func (a *Storage) Open() (storage.Storage, error) {
	switch a.Kind {
	case "local":
		return local.New(a.Local.Dir)
	case "s3":
		creds := s3.Creds{
			Secure:   a.S3.Secure,
			Provider: a.S3.provider(),
			Endpoint: a.S3.Endpoint,
			Region:   a.S3.Region,
			Account:  a.S3.Account,
			Key:      a.S3.Key,
			Secret:   a.S3.Secret,
			Bucket:   a.S3.Bucket,
		}
		return s3.New(creds, a.S3.Prefix)
	}

	return nil, fmt.Errorf("config: BEE_STORAGE %q is not s3 or local", a.Kind)
}

package config

import (
	"errors"
	"fmt"

	"github.com/angels-you/bee/pkg/storage"
	"github.com/angels-you/bee/pkg/storage/local"
	"github.com/angels-you/bee/pkg/storage/s3"
	"github.com/angels-you/bee/pkg/storage/s3/provider"
)

// Storage is the single bee storage: Kind selects local or s3, and only
// the keys of the chosen kind are required
type Storage struct {
	Kind  string     `env:"STORAGE"` // s3 or local, required
	Local LocalStore `prefix:"LOCAL_"`
	S3    S3Store    `prefix:"S3_"`
}

// LocalStore is a folder on disk
type LocalStore struct {
	Dir string `env:"DIR"` // root, required for local
}

// S3Store is one bucket of any s3 provider. Empty Endpoint, Region,
// Account and Prefix are meaningful: provider endpoint and region, no
// account, no prefix
type S3Store struct {
	Provider string `env:"PROVIDER"` // cloudflare, hetzner, aws, minio, ... (pkg/storage/s3/provider), required
	Endpoint string `env:"ENDPOINT"` // empty means the provider template
	Region   string `env:"REGION"`   // empty means the provider region
	Account  string `env:"ACCOUNT"`  // cloudflare account id etc.
	Key      string `env:"KEY"`      // required
	Secret   string `env:"SECRET"`   // required
	Bucket   string `env:"BUCKET"`   // required
	Prefix   string `env:"PREFIX"`   // folder inside the bucket; empty means the root
	Secure   bool   `env:"SECURE"`   // https; only applies to self-hosted (minio, ceph), cloud providers always use https
}

// defaults: storage has no defaults except https
func (a *Storage) defaults() {
	a.S3.Secure = true
}

// validate checks that the kind is known and its keys are set
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

// validate checks the required bucket keys and a known provider
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

// provider returns the provider by name, 0 if unknown
func (a *S3Store) provider() provider.Provider {
	return provider.Provider(provider.ProviderIndex(a.Provider))
}

// Open opens the storage of the chosen kind; an s3 bucket is checked with Ping
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

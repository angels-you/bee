// Package s3 — один бакет любого s3 провайдера (minio-go) и хранилище bee
// поверх него (S3). Список провайдеров с endpoint и регионами — в
// provider
package s3

import (
	"errors"
	"strings"
	"time"

	"github.com/angels-you/bee/pkg/storage/s3/provider"
)

// Creds — доступ к одному бакету. У провайдеров без шаблона endpoint
// (idrive, ceph) Endpoint обязателен
type Creds struct {
	Secure   bool              `json:"secure"`             // https; у облачных провайдеров всегда, настройка только для своих (minio, ceph)
	Provider provider.Provider `json:"provider"`           // provider.Hetzner, Cloudflare, Backblaze...
	Endpoint string            `json:"endpoint,omitempty"` // hel1.your-objectstorage.com; пусто — по шаблону провайдера
	Region   string            `json:"region,omitempty"`   // hel1, us-west-004; пусто — по провайдеру, r2 — всегда auto
	Account  string            `json:"account,omitempty"`  // cloudflare account id
	Key      string            `json:"key"`                // access key / b2 keyID
	Secret   string            `json:"secret"`             // secret key / b2 applicationKey
	Bucket   string            `json:"bucket"`             // my-bucket
	Domain   string            `json:"domain,omitempty"`   // https://cdn.example.com, публичные ссылки; пусто — Upload возвращает ключ
	Timeout  time.Duration     `json:"timeout,omitempty"`  // один запрос, по умолчанию 10s
}

// prepare — дефолты провайдера (регион, endpoint, https, таймаут) и
// проверка
func (a *Creds) prepare() (err error) {

	// регион: r2 всегда auto
	if a.Region == "" || a.Provider == provider.Cloudflare {
		a.Region = a.Provider.Region()
	}

	// endpoint по шаблону провайдера: без региона или аккаунта шаблон не
	// заполнить
	template := a.Provider.Endpoint()
	if a.Endpoint == "" {
		switch {
		case strings.Contains(template, "<account>") && a.Account == "":
			return errors.New("s3: account is required by the endpoint of " + a.Provider.String())
		case strings.Contains(template, "<region>") && a.Region == "":
			return errors.New("s3: region is required by the endpoint of " + a.Provider.String())
		}

		replacer := strings.NewReplacer("<region>", a.Region, "<account>", a.Account)
		a.Endpoint = replacer.Replace(template)
	}

	// облачные провайдеры — всегда https
	if !a.Provider.Hosted() {
		a.Secure = true
	}

	if a.Timeout <= 0 {
		a.Timeout = 10 * time.Second
	}

	a.Domain = strings.TrimRight(a.Domain, "/")

	switch {
	case !a.Provider.Valid():
		return errors.New("s3: provider is not known")
	case a.Endpoint == "":
		return errors.New("s3: endpoint is required")
	case strings.Contains(a.Endpoint, "<"):
		return errors.New("s3: endpoint template needs region or account")
	case a.Key == "":
		return errors.New("s3: key is required")
	case a.Secret == "":
		return errors.New("s3: secret is required")
	case a.Bucket == "":
		return errors.New("s3: bucket is required")
	}

	return nil
}

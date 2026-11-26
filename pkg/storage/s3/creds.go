// Package s3 is one bucket of any s3 provider (minio-go) and the bee storage
// on top of it (S3). The list of providers with endpoints and regions is in
// provider
package s3

import (
	"errors"
	"strings"
	"time"

	"github.com/angels-you/bee/pkg/storage/s3/provider"
)

// Creds is access to one bucket. Providers without an endpoint template
// (idrive, ceph) require Endpoint
type Creds struct {
	Secure   bool              `json:"secure"`             // https; always for cloud providers, the setting is only for self-hosted (minio, ceph)
	Provider provider.Provider `json:"provider"`           // provider.Hetzner, Cloudflare, Backblaze...
	Endpoint string            `json:"endpoint,omitempty"` // hel1.your-objectstorage.com; empty means the provider template
	Region   string            `json:"region,omitempty"`   // hel1, us-west-004; empty means the provider default, r2 is always auto
	Account  string            `json:"account,omitempty"`  // cloudflare account id
	Key      string            `json:"key"`                // access key / b2 keyID
	Secret   string            `json:"secret"`             // secret key / b2 applicationKey
	Bucket   string            `json:"bucket"`             // my-bucket
	Domain   string            `json:"domain,omitempty"`   // https://cdn.example.com, public links; empty means Upload returns the key
	Timeout  time.Duration     `json:"timeout,omitempty"`  // one request, 10s by default
}

// prepare applies provider defaults (region, endpoint, https, timeout) and
// validates
func (a *Creds) prepare() (err error) {

	// region: r2 is always auto
	if a.Region == "" || a.Provider == provider.Cloudflare {
		a.Region = a.Provider.Region()
	}

	// endpoint from the provider template: the template cannot be filled without
	// a region or account
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

	// cloud providers always use https
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

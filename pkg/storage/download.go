package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/angels-you/bee/pkg/types/code"
)

// Downloader downloads an original url into a local file with SSRF protection:
// https only, every hop is resolved and checked for private
// addresses, at most 3 redirects, a hard size limit. The whole download
// is bound by the caller's context. Every error is a *code.Error, and none
// contains the url query (presigned signature). Safe for
// concurrent use
type Downloader struct {
	UserAgent     string                 // User-Agent of every request: bee/1
	MaxSize       int64                  // bytes, 0 means no limit
	AllowInsecure bool                   // http and private addresses: tests only
	HostAllowed   func(host string) bool // nil means any public host

	once   sync.Once
	client *http.Client
}

// Info describes a downloaded file
type Info struct {
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type,omitempty"`
}

// Download writes the url body into file dst and returns its size and sha256. A failed download
// leaves no file behind
func (a *Downloader) Download(ctx context.Context, rawURL, dst string) (info Info, err error) {

	// parse the url
	u, perr := url.Parse(rawURL)
	if perr != nil {
		return info, code.New(code.InvalidJob, "source url is not valid", nil)
	}

	// scheme and host
	err = a.checkURL(u)
	if err != nil {
		return
	}

	// request
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if rerr != nil {
		return info, code.New(code.InvalidJob, "source url is not valid", a.redact(rerr))
	}
	req.Header.Set("User-Agent", a.UserAgent)

	// response
	resp, rerr := a.httpClient().Do(req)
	if rerr != nil {
		return info, a.failed(ctx, rerr)
	}
	defer resp.Body.Close()

	// response status
	err = a.status(resp)
	if err != nil {
		return
	}

	// size known in advance
	if a.MaxSize > 0 && resp.ContentLength > a.MaxSize {
		return info, a.tooLarge()
	}
	info.ContentType = resp.Header.Get("Content-Type")

	// file
	f, ferr := os.Create(dst)
	if ferr != nil {
		return info, code.New(code.InternalTemporaryError, "cannot create the source file", ferr)
	}
	defer a.discard(f, dst, &err)

	// body with a limit
	var body io.Reader = resp.Body
	if a.MaxSize > 0 {
		body = io.LimitReader(resp.Body, a.MaxSize+1)
	}

	// copy and compute sha256 on the fly
	h := sha256.New()
	n, cerr := io.Copy(io.MultiWriter(f, h), body)
	if cerr != nil {
		return info, a.failed(ctx, cerr)
	}

	switch {
	case a.MaxSize > 0 && n > a.MaxSize:
		return info, a.tooLarge()
	case n == 0:
		return info, code.New(code.SourceUnavailable, "source download: empty body", nil)
	}

	info.Size = n
	info.SHA256 = hex.EncodeToString(h.Sum(nil))
	return info, nil
}

// httpClient returns the single downloader client, created on first call
func (a *Downloader) httpClient() *http.Client {
	a.once.Do(a.build)
	return a.client
}

// build returns the client: no proxy, every connection through dial, every
// redirect through redirect
func (a *Downloader) build() {
	transport := &http.Transport{
		Proxy:               nil,
		TLSHandshakeTimeout: 15 * time.Second,
		MaxIdleConns:        4,
		DialContext:         a.dial,
	}

	a.client = &http.Client{
		Transport:     transport,
		CheckRedirect: a.redirect,
	}
}

// dial connects to the first public address of the host; a private one
// is rejected unless AllowInsecure
func (a *Downloader) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// all host addresses
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	// the first one that is allowed and reachable
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var last error
	for _, ip := range ips {
		if !a.AllowInsecure && !a.publicIP(ip.IP) {
			last = code.New(code.InvalidJob, "source url is not allowed: "+host+" resolves to a private address", nil)
			continue
		}

		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}

	if last == nil {
		last = fmt.Errorf("download: no address for %s", host)
	}
	return nil, last
}

// redirect allows at most 3 hops, each by the rules of the first url
func (a *Downloader) redirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("download: too many redirects")
	}

	return a.checkURL(req.URL)
}

// checkURL applies the scheme and host rules to the first request and every
// redirect
func (a *Downloader) checkURL(u *url.URL) error {
	insecureHTTP := a.AllowInsecure && u.Scheme == "http"
	ip := net.ParseIP(u.Hostname())

	switch {
	case u.Host == "":
		return code.New(code.InvalidJob, "source url is not allowed: empty host", nil)
	case u.Scheme != "https" && !insecureHTTP:
		return code.New(code.InvalidJob, "source url is not allowed: scheme "+u.Scheme, nil)
	case ip != nil && !a.AllowInsecure && !a.publicIP(ip):
		return code.New(code.InvalidJob, "source url is not allowed: private address", nil)
	case a.HostAllowed != nil && !a.HostAllowed(u.Hostname()):
		return code.New(code.InvalidJob, "source url is not allowed: host "+u.Hostname(), nil)
	}

	return nil
}

// status maps the response code: 200 or an error with a clear code
func (a *Downloader) status(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusForbidden, http.StatusBadRequest:

		// s3 answers 403 AccessDenied / 400 with "Request has expired" or
		// "X-Amz-Expires ... expired" on an expired signature
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if strings.Contains(strings.ToLower(string(body)), "expire") {
			return code.New(code.SourceUrlExpired, "source url expired", nil)
		}

		return code.New(code.SourceUnavailable, fmt.Sprintf("source access denied: http %d", resp.StatusCode), nil)
	}

	return code.New(code.SourceUnavailable, fmt.Sprintf("source download: http %d", resp.StatusCode), nil)
}

// failed maps a request or body read error: context end, checkURL
// rejection on a redirect, or a network failure; the url in the error has no query
func (a *Downloader) failed(ctx context.Context, err error) error {
	err = a.redact(err)

	// the download context ended
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return code.New(code.DownloadTimeout, "source download timed out", err)
	case ctx.Err() != nil:
		return code.As(ctx.Err())
	}

	// checkURL or dial rejection inside the http client
	var typed *code.Error
	if errors.As(err, &typed) {
		return code.New(typed.Code, typed.Message, err)
	}

	return code.New(code.SourceUnavailable, "source download failed", err)
}

// tooLarge means the original is larger than MaxSize
func (a *Downloader) tooLarge() error {
	return code.New(code.SourceTooLarge, fmt.Sprintf("source is bigger than %d bytes", a.MaxSize), nil)
}

// redact returns the http client error (*url.Error) with the url cut to scheme,
// host and path: the query holds the presigned signature
func (a *Downloader) redact(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}

	// the url does not parse: cut everything after "?"
	u, perr := url.Parse(ue.URL)
	if perr != nil {
		ue.URL, _, _ = strings.Cut(ue.URL, "?")
		return err
	}

	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	ue.URL = u.String()

	return err
}

// discard closes the downloaded file and removes it if the download
// failed (*err is set)
func (a *Downloader) discard(f *os.File, name string, err *error) {
	f.Close()

	if *err != nil {
		os.Remove(name)
	}
}

// publicIP reports whether ip is not loopback, private, link-local, multicast or
// unspecified
func (a *Downloader) publicIP(ip net.IP) bool {
	switch {
	case ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsMulticast(),
		ip.IsUnspecified(),
		ip.IsInterfaceLocalMulticast():
		return false
	}

	// carrier-grade nat 100.64.0.0/10, 0.0.0.0/8 and 169.254.0.0/16
	v4 := ip.To4()
	if v4 == nil {
		return true
	}

	switch {
	case v4[0] == 100 && v4[1]&0xc0 == 64:
		return false
	case v4[0] == 0:
		return false
	case v4[0] == 169 && v4[1] == 254:
		return false
	}

	return true
}

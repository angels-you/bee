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

// Downloader — скачивает url оригинала в локальный файл с защитой от SSRF:
// только https, каждый хоп резолвится и проверяется на приватные
// адреса, не больше 3 редиректов, жёсткий лимит размера. Всё скачивание
// ограничено контекстом вызывающего. Каждая ошибка — *code.Error, и ни
// одна не содержит query url (подпись presigned). Безопасен для
// одновременного использования
type Downloader struct {
	UserAgent     string                 // User-Agent каждого запроса: bee/1
	MaxSize       int64                  // байт, 0 — без лимита
	AllowInsecure bool                   // http и приватные адреса: только тесты
	HostAllowed   func(host string) bool // nil — любой публичный хост

	once   sync.Once
	client *http.Client
}

// Info — скачанный файл
type Info struct {
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type,omitempty"`
}

// Download — тело url в файл dst, его размер и sha256. Неудачная загрузка
// файл за собой не оставляет
func (a *Downloader) Download(ctx context.Context, rawURL, dst string) (info Info, err error) {

	// разбираем url
	u, perr := url.Parse(rawURL)
	if perr != nil {
		return info, code.New(code.InvalidJob, "source url is not valid", nil)
	}

	// схема и хост
	err = a.checkURL(u)
	if err != nil {
		return
	}

	// запрос
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if rerr != nil {
		return info, code.New(code.InvalidJob, "source url is not valid", a.redact(rerr))
	}
	req.Header.Set("User-Agent", a.UserAgent)

	// ответ
	resp, rerr := a.httpClient().Do(req)
	if rerr != nil {
		return info, a.failed(ctx, rerr)
	}
	defer resp.Body.Close()

	// статус ответа
	err = a.status(resp)
	if err != nil {
		return
	}

	// размер известен заранее
	if a.MaxSize > 0 && resp.ContentLength > a.MaxSize {
		return info, a.tooLarge()
	}
	info.ContentType = resp.Header.Get("Content-Type")

	// файл
	f, ferr := os.Create(dst)
	if ferr != nil {
		return info, code.New(code.InternalTemporaryError, "cannot create the source file", ferr)
	}
	defer a.discard(f, dst, &err)

	// тело с лимитом
	var body io.Reader = resp.Body
	if a.MaxSize > 0 {
		body = io.LimitReader(resp.Body, a.MaxSize+1)
	}

	// копируем и сразу считаем sha256
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

// httpClient — единственный клиент загрузчика, создаётся при первом вызове
func (a *Downloader) httpClient() *http.Client {
	a.once.Do(a.build)
	return a.client
}

// build — клиент: без прокси, каждое соединение через dial, каждый
// редирект через redirect
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

// dial — соединение с первым публичным адресом хоста; приватный
// отклоняется, если не AllowInsecure
func (a *Downloader) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// все адреса хоста
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	// первый, к которому можно и удалось подключиться
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

// redirect — не больше 3 хопов, каждый по правилам первого url
func (a *Downloader) redirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("download: too many redirects")
	}

	return a.checkURL(req.URL)
}

// checkURL — правила схемы и хоста для первого запроса и каждого
// редиректа
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

// status — код ответа: 200 или ошибка с понятным кодом
func (a *Downloader) status(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusForbidden, http.StatusBadRequest:

		// s3 отвечает 403 AccessDenied / 400 с "Request has expired" или
		// "X-Amz-Expires ... expired" на истёкшую подпись
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if strings.Contains(strings.ToLower(string(body)), "expire") {
			return code.New(code.SourceUrlExpired, "source url expired", nil)
		}

		return code.New(code.SourceUnavailable, fmt.Sprintf("source access denied: http %d", resp.StatusCode), nil)
	}

	return code.New(code.SourceUnavailable, fmt.Sprintf("source download: http %d", resp.StatusCode), nil)
}

// failed — ошибка запроса или чтения тела: конец контекста, отказ
// checkURL на редиректе или сетевой сбой; url в ошибке без query
func (a *Downloader) failed(ctx context.Context, err error) error {
	err = a.redact(err)

	// контекст загрузки закончился
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return code.New(code.DownloadTimeout, "source download timed out", err)
	case ctx.Err() != nil:
		return code.As(ctx.Err())
	}

	// отказ checkURL или dial внутри http клиента
	var typed *code.Error
	if errors.As(err, &typed) {
		return code.New(typed.Code, typed.Message, err)
	}

	return code.New(code.SourceUnavailable, "source download failed", err)
}

// tooLarge — оригинал больше MaxSize
func (a *Downloader) tooLarge() error {
	return code.New(code.SourceTooLarge, fmt.Sprintf("source is bigger than %d bytes", a.MaxSize), nil)
}

// redact — ошибка http клиента (*url.Error) с url, обрезанным до схемы,
// хоста и пути: в query лежит подпись presigned
func (a *Downloader) redact(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}

	// url не разбирается — режем всё после "?"
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

// discard — закрывает скачанный файл и удаляет его, если загрузка не
// удалась (*err задан)
func (a *Downloader) discard(f *os.File, name string, err *error) {
	f.Close()

	if *err != nil {
		os.Remove(name)
	}
}

// publicIP — не loopback, не приватный, не link-local, не multicast и не
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

	// carrier-grade nat 100.64.0.0/10, 0.0.0.0/8 и 169.254.0.0/16
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

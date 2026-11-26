package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angels-you/bee/pkg/types/code"
)

// fixture is a test server of originals and its body
type fixture struct {
	body   []byte
	secret string
}

// init sets the original body and the presigned signature of test urls
func (a *fixture) init() {
	a.body = bytes.Repeat([]byte("audio"), 1000)
	a.secret = "X-Amz-Signature=TOPSECRET"
}

// serve returns test server responses by path
func (a *fixture) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/agent":
		w.Write([]byte(r.UserAgent()))
	case "/ok":
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(a.body)
	case "/expired":
		w.WriteHeader(403)
		w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Request has expired</Message></Error>`))
	case "/denied":
		w.WriteHeader(403)
		w.Write([]byte(`<Error><Code>AccessDenied</Code></Error>`))
	case "/redirect":
		http.Redirect(w, r, "/ok", http.StatusFound)
	case "/loop":
		http.Redirect(w, r, "/loop?"+a.secret, http.StatusFound)
	case "/slow":
		time.Sleep(300 * time.Millisecond)
		w.Write(a.body)
	case "/empty":
	default:
		w.WriteHeader(404)
	}
}

// codeOf returns the downloader error code
func (a *fixture) codeOf(err error) code.Code {
	var e *code.Error
	if !errors.As(err, &e) {
		return ""
	}

	return e.Code
}

func TestDownload(t *testing.T) {
	f := fixture{}
	f.init()

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()

	ctx := context.Background()
	dir := t.TempDir()
	d := &Downloader{UserAgent: "test/1", MaxSize: 10000, AllowInsecure: true}

	// normal download
	info, err := d.Download(ctx, srv.URL+"/ok", filepath.Join(dir, "ok"))
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(f.body)
	switch {
	case info.Size != int64(len(f.body)):
		t.Errorf("size %d", info.Size)
	case info.SHA256 != hex.EncodeToString(sum[:]):
		t.Errorf("sha256 %s", info.SHA256)
	case info.ContentType != "audio/wav":
		t.Errorf("content type %s", info.ContentType)
	}

	// user agent
	_, err = d.Download(ctx, srv.URL+"/agent", filepath.Join(dir, "agent"))
	if err != nil {
		t.Fatal(err)
	}

	ua, err := os.ReadFile(filepath.Join(dir, "agent"))
	if err != nil || string(ua) != "test/1" {
		t.Errorf("user agent %q %v", ua, err)
	}

	// one redirect
	_, err = d.Download(ctx, srv.URL+"/redirect", filepath.Join(dir, "r"))
	if err != nil {
		t.Errorf("one redirect must work: %v", err)
	}

	// error codes
	cases := map[string]code.Code{
		"/expired": code.SourceUrlExpired,
		"/denied":  code.SourceUnavailable,
		"/missing": code.SourceUnavailable,
		"/loop":    code.SourceUnavailable,
		"/empty":   code.SourceUnavailable,
	}
	for p, want := range cases {
		_, err := d.Download(ctx, srv.URL+p, filepath.Join(dir, "x"))
		got := f.codeOf(err)
		if got != want {
			t.Errorf("%s: %v, want %s", p, err, want)
		}
	}

	// too large
	small := &Downloader{MaxSize: 100, AllowInsecure: true}
	_, err = small.Download(ctx, srv.URL+"/ok", filepath.Join(dir, "x"))
	if f.codeOf(err) != code.SourceTooLarge {
		t.Errorf("too large: %v", err)
	}

	_, err = os.Stat(filepath.Join(dir, "x"))
	if err == nil {
		t.Error("failed download must not leave a file")
	}

	// download timeout
	tctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err = d.Download(tctx, srv.URL+"/slow", filepath.Join(dir, "x"))
	if f.codeOf(err) != code.DownloadTimeout {
		t.Errorf("timeout: %v", err)
	}

	// production mode: http and loopback are rejected before the request
	strict := &Downloader{}
	_, err = strict.Download(ctx, srv.URL+"/ok", filepath.Join(dir, "x"))
	if f.codeOf(err) != code.InvalidJob {
		t.Errorf("http must be refused: %v", err)
	}

	_, err = strict.Download(ctx, "https://127.0.0.1/ok", filepath.Join(dir, "x"))
	if f.codeOf(err) != code.InvalidJob {
		t.Errorf("loopback must be refused: %v", err)
	}

	strict.HostAllowed = func(h string) bool {
		return strings.HasSuffix(h, ".r2.cloudflarestorage.com")
	}
	_, err = strict.Download(ctx, "https://example.com/ok", filepath.Join(dir, "x"))
	if f.codeOf(err) != code.InvalidJob {
		t.Errorf("foreign host must be refused: %v", err)
	}
}

// TestDownloadErrorsKeepQueryOut checks that an http client error names the url
// it failed on: the first one (connection refused) and the redirect target (too many
// redirects); none contains the query (presigned signature), userinfo or
// fragment, and the cause stays available
func TestDownloadErrorsKeepQueryOut(t *testing.T) {
	f := fixture{}
	f.init()

	loop := httptest.NewServer(http.HandlerFunc(f.serve))
	defer loop.Close()

	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	ctx := context.Background()
	d := &Downloader{UserAgent: "test/1", AllowInsecure: true}
	dst := filepath.Join(t.TempDir(), "x")

	urls := []string{
		deadURL + "/a.wav?" + f.secret + "#frag",
		loop.URL + "/loop?" + f.secret,
	}
	for _, u := range urls {
		_, err := d.Download(ctx, u, dst)

		var ue *url.Error
		switch {
		case err == nil:
			t.Errorf("%s: no error", u)
		case strings.Contains(err.Error(), "TOPSECRET"):
			t.Errorf("query in the error: %v", err)
		case strings.Contains(err.Error(), "frag"):
			t.Errorf("fragment in the error: %v", err)
		case !errors.As(err, &ue) || ue.Err == nil:
			t.Errorf("the cause is lost: %v", err)
		}
	}

	// the url does not parse: the error has no url text
	_, err := d.Download(ctx, "http://user:TOPSECRET@[::1", dst)
	switch {
	case f.codeOf(err) != code.InvalidJob:
		t.Errorf("unparsable url: %v", err)
	case strings.Contains(err.Error(), "TOPSECRET"):
		t.Errorf("unparsable url in the error: %v", err)
	}
}

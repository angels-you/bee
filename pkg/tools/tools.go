// Package tools holds small context-free bee utilities: storage keys and
// urls. No dependencies, the public job types import it too
package tools

import (
	"net/url"
	"path"
	"strings"
)

// CleanKey returns a storage key in normal form: forward slashes, no ".." or
// "//", no "/" at the edges: "a//b/../c" → "a/c"
func CleanKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "\\", "/")

	return strings.Trim(path.Clean("/"+key), "/")
}

// Key validates a result key from a job: one leading "/" is
// allowed and stripped, the rest must already be a clean relative
// key. ok is false if the key is empty or dirty
func Key(raw string) (key string, ok bool) {
	key = strings.TrimSpace(raw)
	key = strings.TrimPrefix(key, "/")

	switch {
	case key == "":
		return "", false
	case key == ".":
		return "", false
	case path.IsAbs(key):
		return "", false
	case key != path.Clean(key):
		return "", false
	case strings.Contains(key, ".."):
		return "", false
	case strings.Contains(key, "//"):
		return "", false
	case strings.ContainsAny(key, " \t\r\n?#\\"):
		return "", false
	}

	return key, true
}

// URL parses a url from a job and checks it has a host; the scheme and host are
// checked by the downloader according to bee settings
func URL(raw string) (u *url.URL, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, false
	}

	if u.Host == "" {
		return nil, false
	}

	return u, true
}

// SafeURL returns a url for logs: without query (presigned signature), userinfo and
// fragment
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}

	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""

	return u.String()
}

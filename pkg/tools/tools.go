// Package tools holds small context-free bee utilities: storage keys and
// urls. No dependencies, the public job types import it too
package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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

// ID reports whether id is a valid job id: a string of 1..128 printable
// characters (any script, spaces, no line breaks or other control
// characters) or a whole number (an int type, a float without a fraction,
// json.Number). nil, bool, objects, arrays and the empty string are not.
// The same rule on every transport
func ID(id any) bool {
	switch v := id.(type) {
	case string:
		return idText(v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return float64(v) == math.Trunc(float64(v))
	case float64:
		return v == math.Trunc(v) && !math.IsInf(v, 0)
	case json.Number:
		return idNumber(v)
	}

	return false
}

// IDString is the id as text for logs and metrics: a string as is, a
// number in its digits
func IDString(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case nil:
		return ""
	}

	return fmt.Sprint(id)
}

// idText reports whether s is 1..128 printable characters of valid utf-8
func idText(s string) bool {
	switch {
	case !utf8.ValidString(s):
		return false
	case s == "":
		return false
	case utf8.RuneCountInString(s) > 128:
		return false
	}

	// printable only
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}

	return true
}

// idNumber reports whether n is a whole number: an integer of any size or
// a float without a fraction
func idNumber(n json.Number) bool {
	_, err := strconv.ParseInt(n.String(), 10, 64)
	if err == nil {
		return true
	}

	f, err := n.Float64()
	if err != nil {
		return false
	}

	return f == math.Trunc(f) && !math.IsInf(f, 0)
}

// Decode reads json into v keeping numbers as json.Number: a job id or a
// meta value comes back exactly as it was sent, big integers included
func Decode(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()

	return d.Decode(v)
}

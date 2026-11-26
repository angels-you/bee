package tools

import (
	"encoding/json"
	"testing"
)

// TestCleanKey checks that a storage key is normalized
func TestCleanKey(t *testing.T) {
	cases := map[string]string{
		"/a/b/../c.json": "a/c.json",
		"a//b":           "a/b",
		" a\\b ":         "a/b",
		"../../x":        "x",
		"":               "",
		"/":              "",
	}

	for in, want := range cases {
		got := CleanKey(in)
		if got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// TestKey checks a job key: one "/" is stripped, a dirty key is rejected
func TestKey(t *testing.T) {
	good := map[string]string{
		"a/b.m3u8":  "a/b.m3u8",
		"/a/b.m3u8": "a/b.m3u8",
		" x.jpg ":   "x.jpg",
	}

	for in, want := range good {
		got, ok := Key(in)
		if !ok || got != want {
			t.Errorf("%q → %q %v, want %q", in, got, ok, want)
		}
	}

	bad := []string{"", "/", ".", "//a", "a//b", "a/../b", "../a", "a/./b", "a b", "a?b", "a#b", "a\\b", "a/"}
	for _, in := range bad {
		_, ok := Key(in)
		if ok {
			t.Errorf("%q must be refused", in)
		}
	}
}

// TestURL checks that a url with a host is accepted, without a host it is not
func TestURL(t *testing.T) {
	_, ok := URL("https://example.com/a.wav?sig=1")
	if !ok {
		t.Error("valid url refused")
	}

	for _, in := range []string{"", "a.wav", "https://", "::"} {
		_, ok = URL(in)
		if ok {
			t.Errorf("%q must be refused", in)
		}
	}
}

// TestSafeURL checks that logs have no signature, userinfo or fragment
func TestSafeURL(t *testing.T) {
	got := SafeURL("https://user:pass@example.com/a.wav?X-Amz-Signature=SECRET#frag")
	if got != "https://example.com/a.wav" {
		t.Errorf("safe url %q", got)
	}
}

// TestID checks the job id rule
func TestID(t *testing.T) {
	good := []string{"a", "track-1", "01J9Z3K6", "550e8400-e29b-41d4-a716-446655440000", "a_b", "a.b", "a b", "трек 1", "user/42:photo"}
	for _, id := range good {
		if !ID(id) {
			t.Errorf("%q refused", id)
		}
	}

	long := make([]byte, 129)
	for i := range long {
		long[i] = 'a'
	}

	bad := []string{"", "a\nb", "a\tb", "a\x00", string([]byte{0xff}), string(long)}
	for _, id := range bad {
		if ID(id) {
			t.Errorf("%q accepted", id)
		}
	}
}

// TestIDNumbers checks numbers, other types and the text form of ids
func TestIDNumbers(t *testing.T) {
	good := []any{1, int64(9007199254740993), uint8(3), 2.0, json.Number("9007199254740993"), json.Number("12")}
	for _, id := range good {
		if !ID(id) {
			t.Errorf("%v refused", id)
		}
	}

	bad := []any{nil, true, 1.5, json.Number("1.5"), map[string]any{}, []any{1}}
	for _, id := range bad {
		if ID(id) {
			t.Errorf("%v accepted", id)
		}
	}

	text := map[string]any{
		"abc":              "abc",
		"9007199254740993": json.Number("9007199254740993"),
		"42":               42,
		"3":                3.0,
	}
	for want, id := range text {
		if IDString(id) != want {
			t.Errorf("%v as %q", id, IDString(id))
		}
	}
}

// TestDecode checks that numbers keep their digits
func TestDecode(t *testing.T) {
	var v map[string]any
	err := Decode([]byte(`{"id":9007199254740993}`), &v)
	if err != nil || v["id"] != json.Number("9007199254740993") {
		t.Errorf("decode %v %v", v, err)
	}
}

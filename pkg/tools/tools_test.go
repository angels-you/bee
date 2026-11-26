package tools

import "testing"

// TestCleanKey — ключ хранилища приводится к нормальному виду
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

// TestKey — ключ из job: один "/" срезается, грязный ключ отказ
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

// TestURL — url с хостом принимается, без хоста нет
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

// TestSafeURL — в логах нет подписи, userinfo и fragment
func TestSafeURL(t *testing.T) {
	got := SafeURL("https://user:pass@example.com/a.wav?X-Amz-Signature=SECRET#frag")
	if got != "https://example.com/a.wav" {
		t.Errorf("safe url %q", got)
	}
}

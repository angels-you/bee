// Package tools — мелкие утилиты bee без контекста: ключи хранилища и
// url. Без зависимостей, его импортируют и публичные типы job
package tools

import (
	"net/url"
	"path"
	"strings"
)

// CleanKey — ключ хранилища в нормальном виде: прямые слэши, без ".." и
// "//", без "/" по краям: "a//b/../c" → "a/c"
func CleanKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "\\", "/")

	return strings.Trim(path.Clean("/"+key), "/")
}

// Key — проверка ключа результата из job: один "/" в начале
// допускается и срезается, остальное должно быть уже чистым относительным
// ключом. ok false — ключ пустой или грязный
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

// URL — url из job разобран и у него есть хост; схема и хост проверяет
// уже загрузчик по настройкам bee
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

// SafeURL — url для логов: без query (подпись presigned), userinfo и
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

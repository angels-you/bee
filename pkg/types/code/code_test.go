package code

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// TestRetryable — повторяемые коды; каждый из них, кроме истёкшего url,
// ещё и передоставляется, и только повторяемый код передоставляется
func TestRetryable(t *testing.T) {
	retry := map[Code]bool{
		DownloadTimeout:        true,
		SourceUrlExpired:       true,
		SourceUnavailable:      true,
		StorageTimeout:         true,
		StorageUnavailable:     true,
		InternalTemporaryError: true,
	}

	for _, c := range CodeIndexes() {
		if c.Retryable() != retry[c] {
			t.Errorf("%s retryable %v", c, c.Retryable())
		}

		if New(c, "m", nil).Retryable != retry[c] {
			t.Errorf("%s: Error.Retryable differs from the code", c)
		}

		if c.Redeliver() != (retry[c] && c != SourceUrlExpired) {
			t.Errorf("%s redeliver %v", c, c.Redeliver())
		}

		if c.Desc() == "" {
			t.Errorf("%s has no description", c)
		}
	}

	if len(CodeIndexes()) != 26 {
		t.Errorf("%d codes", len(CodeIndexes()))
	}
}

// TestAs — типизированная ошибка проходит даже обёрнутой, всё остальное —
// временная внутренняя; причина в json не попадает
func TestAs(t *testing.T) {
	typed := New(InvalidPath, "bad", nil)

	got := As(fmt.Errorf("wrapped: %w", typed))
	if got.Code != InvalidPath {
		t.Errorf("wrapped typed error → %s", got.Code)
	}

	for _, err := range []error{context.DeadlineExceeded, context.Canceled, errors.New("boom")} {
		got = As(err)
		if got.Code != InternalTemporaryError {
			t.Errorf("%v → %s", err, got.Code)
		}
	}

	if As(nil) != nil {
		t.Error("nil is an error")
	}

	js, err := json.Marshal(New(VerifyFailed, "bad output", errors.New("secret cause")))
	if err != nil {
		t.Fatal(err)
	}

	if string(js) != `{"code":"VERIFY_FAILED","message":"bad output","retryable":false}` {
		t.Errorf("json %s", js)
	}
}

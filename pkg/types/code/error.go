// Package code — коды ошибок bee (список в файле "code", code.go
// генерируется командой `const code`) и Error, типизированная ошибка job.
// Одна таблица на все типы job: audio, video, images.
//
// Можно ли повторить — свойство кода (Code.Retryable), и можно ли ту же
// job доставить ещё раз — тоже (Code.Redeliver): истёкший url повторяется
// только новой job со свежей ссылкой.
package code

import (
	"context"
	"errors"
	"fmt"
)

// Error — упавшая job. Retryable повторяет Code.Retryable, чтобы клиенту
// не нужна была таблица кодов. Err — причина, в json никогда не попадает:
// в ней может быть вывод ffmpeg или url
type Error struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Err       error  `json:"-"`
}

// New — ошибка с кодом c; err — причина, nil если её нет
func New(c Code, message string, err error) *Error {
	return &Error{
		Code:      c,
		Message:   message,
		Retryable: c.Retryable(),
		Err:       err,
	}
}

// Error — код, сообщение и причина, если она есть
func (a *Error) Error() string {
	if a.Err != nil {
		return fmt.Sprintf("%s: %s: %v", a.Code, a.Message, a.Err)
	}

	return fmt.Sprintf("%s: %s", a.Code, a.Message)
}

// Unwrap — причина
func (a *Error) Unwrap() error {
	return a.Err
}

// As — любая ошибка как *Error: типизированная как есть (даже обёрнутая),
// конец контекста и всё остальное — INTERNAL_TEMPORARY_ERROR
func As(err error) *Error {
	if err == nil {
		return nil
	}

	// уже типизированная, возможно обёрнутая
	var e *Error
	if errors.As(err, &e) {
		return e
	}

	// контекст job закончился или неожиданный сбой
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return New(InternalTemporaryError, "job timed out", err)
	case errors.Is(err, context.Canceled):
		return New(InternalTemporaryError, "job cancelled", err)
	}

	return New(InternalTemporaryError, err.Error(), err)
}

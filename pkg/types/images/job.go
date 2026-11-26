// Package images — job картинок bee и её результат, одинаковые на любом
// транспорте (http, grpc, nats). Без зависимостей от сервера: пакет
// импортирует клиент
package images

import (
	"fmt"

	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/code"
)

// Job — один оригинал и результаты, которые из него сделать. Input —
// url оригинала (presigned GET или любой публичный https), в логах без
// query
type Job struct {
	ID    string `json:"id"`
	Input string `json:"input"` // url оригинала
	Tasks []Task `json:"tasks"` // хотя бы одна
}

// Task — один результат. ID и Output уникальны в job; Output — ключ в
// хранилище bee, который выбирает клиент (один "/" в начале срезается,
// остальное — чистый относительный ключ). Name — метка клиента,
// возвращается как есть
type Task struct {
	ID      string  `json:"id"`
	Name    string  `json:"name,omitempty"`
	Output  string  `json:"output"`
	Profile Profile `json:"profile"`
}

// Validate — job без учёта лимитов bee: id, url, задачи и их профили.
// Ключи результатов приводятся к чистому виду на месте. Схему и хост
// url, размеры против лимитов проверяет движок
func (a *Job) Validate() *code.Error {
	switch {
	case a.ID == "":
		return code.New(code.InvalidJob, "id is required", nil)
	case len(a.Tasks) == 0:
		return code.New(code.InvalidJob, "at least one task is required", nil)
	}

	// url оригинала
	_, ok := tools.URL(a.Input)
	if !ok {
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	}

	// задачи: id и ключи уникальны
	ids := map[string]bool{}
	outputs := map[string]bool{}
	for i := range a.Tasks {
		t := &a.Tasks[i]

		e := t.validate(i)
		if e != nil {
			return e
		}

		switch {
		case ids[t.ID]:
			return code.New(code.InvalidJob, "task id "+t.ID+" is used twice", nil)
		case outputs[t.Output]:
			return code.New(code.InvalidPath, "task "+t.ID+": output is used by another task", nil)
		}

		ids[t.ID] = true
		outputs[t.Output] = true
	}

	return nil
}

// validate — одна задача; i — её номер для ошибки без id
func (a *Task) validate(i int) *code.Error {
	if a.ID == "" {
		return code.New(code.InvalidJob, fmt.Sprintf("task %d has no id", i), nil)
	}

	// ключ результата
	key, ok := tools.Key(a.Output)
	if !ok {
		return code.New(code.InvalidPath, "task "+a.ID+": output must be a clean relative storage key", nil)
	}
	a.Output = key

	// профиль
	e := a.Profile.Validate()
	if e != nil {
		return code.New(e.Code, "task "+a.ID+": "+e.Message, nil)
	}

	return nil
}

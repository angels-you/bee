// Package images holds the images bee job and its result, the same on any
// transport (http, grpc, nats). No server dependencies: the client imports
// this package
package images

import (
	"fmt"

	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/code"
)

// Job is one original and the results to make from it. Input is the url
// of the original (presigned GET or any public https), logged without the
// query
type Job struct {
	ID    any            `json:"id"`             // a string or a number, returned as sent
	Meta  map[string]any `json:"meta,omitempty"` // data of the caller, returned untouched in the result
	Input string         `json:"input"`          // url of the original
	Tasks []Task         `json:"tasks"`          // at least one
}

// Task is one result. ID and Output are unique within the job; Output is a
// key in the bee storage chosen by the client (one leading "/" is trimmed,
// the rest is a clean relative key). Name is a client label, returned as
// is
type Task struct {
	ID      string  `json:"id"`
	Name    string  `json:"name,omitempty"`
	Output  string  `json:"output"`
	Profile Profile `json:"profile"`
}

// Validate checks the job regardless of bee limits: id, url, tasks and
// their profiles. Result keys are cleaned in place. The url scheme and
// host, sizes against limits are checked by the engine
func (a *Job) Validate() *code.Error {
	switch {
	case !tools.ID(a.ID):
		return code.New(code.InvalidJob, "id must be 1..128 printable characters", nil)
	case len(a.Tasks) == 0:
		return code.New(code.InvalidJob, "at least one task is required", nil)
	}

	// url of the original
	_, ok := tools.URL(a.Input)
	if !ok {
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	}

	// tasks: ids and keys are unique
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

// validate checks one task; i is its index for an error without id
func (a *Task) validate(i int) *code.Error {
	if a.ID == "" {
		return code.New(code.InvalidJob, fmt.Sprintf("task %d has no id", i), nil)
	}

	// result key
	key, ok := tools.Key(a.Output)
	if !ok {
		return code.New(code.InvalidPath, "task "+a.ID+": output must be a clean relative storage key", nil)
	}
	a.Output = key

	// profile
	e := a.Profile.Validate()
	if e != nil {
		return code.New(e.Code, "task "+a.ID+": "+e.Message, nil)
	}

	return nil
}

package video

import (
	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/code"
)

// Validate checks what a Stream needs before it is sent: the id, the url
// of the original and the output key. bee checks everything else
// (profiles, options, limits) when the job runs
func (a *Stream) Validate() *code.Error {
	_, url := tools.URL(a.Input)
	_, key := tools.Key(a.Output)

	switch {
	case !tools.ID(a.ID):
		return code.New(code.InvalidJob, "id must be 1..128 printable characters", nil)
	case !url:
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	case !key:
		return code.New(code.InvalidPath, "output must be a clean relative storage key", nil)
	}

	return nil
}

// Validate checks what a Video needs before it is sent: the id, the url of
// the original and the output key. bee checks everything else when the
// job runs
func (a *Video) Validate() *code.Error {
	_, url := tools.URL(a.Input)
	_, key := tools.Key(a.Output)

	switch {
	case !tools.ID(a.ID):
		return code.New(code.InvalidJob, "id must be 1..128 printable characters", nil)
	case !url:
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	case !key:
		return code.New(code.InvalidPath, "output must be a clean relative storage key", nil)
	}

	return nil
}

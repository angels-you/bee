package video

import (
	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/code"
)

// Validate checks what a Stream needs before it is sent: the id, the url
// of the original and the output key. bee checks everything else
// (profiles, options, limits) when the job runs
func (a *StreamJob) Validate() *code.Error {
	_, url := tools.URL(a.Input)
	_, key := tools.Key(a.PlaylistPath)

	switch {
	case !tools.ID(a.ID):
		return code.New(code.InvalidJob, "id must be 1..128 printable characters", nil)
	case !url:
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	case !key:
		return code.New(code.InvalidPath, "playlist_path must be a clean relative storage key", nil)
	}

	return nil
}

// Validate checks what a Video needs before it is sent: the id, the url of
// the original and the output key. bee checks everything else when the
// job runs
func (a *VideoJob) Validate() *code.Error {
	_, url := tools.URL(a.Input)
	_, key := tools.Key(a.FilePath)

	switch {
	case !tools.ID(a.ID):
		return code.New(code.InvalidJob, "id must be 1..128 printable characters", nil)
	case !url:
		return code.New(code.InvalidJob, "input is not a valid url", nil)
	case !key:
		return code.New(code.InvalidPath, "file_path must be a clean relative storage key", nil)
	}

	return nil
}

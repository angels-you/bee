package proto

import (
	"encoding/json"
	"github.com/angels-you/bee/pkg/tools"

	"github.com/angels-you/bee/pkg/types/code"
)

// Failure fills res (a pointer to audio.Result, video.Result or
// images.Result) as the result of a failed job: its id, its meta and e. Clients use it for failures they see themselves (transport,
// status without a result).
func Failure(res any, id any, meta map[string]any, e *code.Error) {
	data, _ := json.Marshal(struct {
		ID    any            `json:"id"`
		Meta  map[string]any `json:"meta,omitempty"`
		Error *code.Error    `json:"error"`
	}{id, meta, e})

	tools.Decode(data, res)
}

// ErrorOf reads the error of the json of a result; nil when the job is
// done.
func ErrorOf(result []byte) *code.Error {
	var head struct {
		Error *code.Error `json:"error"`
	}
	json.Unmarshal(result, &head)

	return head.Error
}

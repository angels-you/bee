package proto

import (
	"encoding/json"
	"fmt"

	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Envelope is the json of one nats message, both ways:
//
//	jobs   {"kind":"audio","job":{...the job...}}
//	done   {"kind":"audio","id":"track-1","result":{...the result...}}
//
// Kind is audio, stream, video or images; Job is the json of audio.Job,
// video.Stream, video.Video or images.Job; Result is the json of the result
// of the kind, with "error" set when the job failed. ID is the id of the
// job the result belongs to
type Envelope struct {
	Kind   Kind            `json:"kind"`
	ID     string          `json:"id,omitempty"`
	Job    json.RawMessage `json:"job,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// Audio is the result of an audio job
func (a Envelope) Audio() (audio.Result, error) {
	var res audio.Result
	err := a.decode(&res, Audio)

	return res, err
}

// Video is the result of a stream or video job
func (a Envelope) Video() (video.Result, error) {
	var res video.Result

	// stream and video share one result
	kind := Video
	if a.Kind == Stream {
		kind = Stream
	}

	err := a.decode(&res, kind)
	return res, err
}

// Images is the result of an images job
func (a Envelope) Images() (images.Result, error) {
	var res images.Result
	err := a.decode(&res, Images)

	return res, err
}

// Failure is the error of the job, nil when it is done
func (a Envelope) Failure() *code.Error {
	return ErrorOf(a.Result)
}

// decode reads Result into res when the envelope is of kind
func (a Envelope) decode(res any, kind Kind) error {
	if a.Kind != kind {
		return fmt.Errorf("the envelope is %q, not %q", a.Kind, kind)
	}

	return json.Unmarshal(a.Result, res)
}

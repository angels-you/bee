package engine

import (
	"context"
	"encoding/json"
	"github.com/angels-you/bee/pkg/tools"

	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Do runs a job of type kind from json and returns the result as json: one entry
// for all transports (http, grpc, nats). A failed job returns the result json
// with error and the same *code.Error. Input that is not a job gives INVALID_JOB
// with the id, if it could be read
func (a *Engine) Do(ctx context.Context, kind proto.Kind, data []byte) ([]byte, error) {
	switch kind {
	case proto.Audio:
		return a.doAudio(ctx, data)
	case proto.Stream:
		return a.doStream(ctx, data)
	case proto.Video:
		return a.doVideo(ctx, data)
	case proto.Images:
		return a.doImages(ctx, data)
	}

	return a.malformed(data, "unknown job type "+string(kind))
}

// doAudio runs an audio job from json
func (a *Engine) doAudio(ctx context.Context, data []byte) ([]byte, error) {
	var j audio.Job
	err := tools.Decode(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Audio(ctx, j)
	return a.reply(res, err)
}

// doStream runs an HLS job from json
func (a *Engine) doStream(ctx context.Context, data []byte) ([]byte, error) {
	var j video.StreamJob
	err := tools.Decode(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Stream(ctx, j)
	return a.reply(res, err)
}

// doVideo runs an MP4 job from json
func (a *Engine) doVideo(ctx context.Context, data []byte) ([]byte, error) {
	var j video.VideoJob
	err := tools.Decode(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Video(ctx, j)
	return a.reply(res, err)
}

// doImages runs an image job from json
func (a *Engine) doImages(ctx context.Context, data []byte) ([]byte, error) {
	var j images.Job
	err := tools.Decode(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Images(ctx, j)
	return a.reply(res, err)
}

// reply returns the result as json and the job error as is
func (a *Engine) reply(res any, err error) ([]byte, error) {
	data, jerr := json.Marshal(res)
	if jerr != nil {
		e := code.New(code.InternalTemporaryError, "result json", jerr)
		return nil, e
	}

	return data, err
}

// malformed handles a non-job: a result with the id (if readable) and INVALID_JOB
func (a *Engine) malformed(data []byte, message string) ([]byte, error) {
	var head struct {
		ID any `json:"id"`
	}
	tools.Decode(data, &head)

	e := code.New(code.InvalidJob, message, nil)
	res := struct {
		ID    any         `json:"id"`
		Error *code.Error `json:"error"`
	}{head.ID, e}

	out, _ := json.Marshal(res)
	return out, e
}

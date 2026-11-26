package engine

import (
	"context"
	"encoding/json"

	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Do — job типа kind в json, результат в json: один вход для всех
// транспортов (http, grpc, nats). Упавшая job — json результата с error
// и тот же *code.Error. Не json job — INVALID_JOB с id, если его удалось
// прочитать
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

// doAudio — аудио job из json
func (a *Engine) doAudio(ctx context.Context, data []byte) ([]byte, error) {
	var j audio.Job
	err := json.Unmarshal(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Audio(ctx, j)
	return a.reply(res, err)
}

// doStream — HLS job из json
func (a *Engine) doStream(ctx context.Context, data []byte) ([]byte, error) {
	var j video.Stream
	err := json.Unmarshal(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Stream(ctx, j)
	return a.reply(res, err)
}

// doVideo — MP4 job из json
func (a *Engine) doVideo(ctx context.Context, data []byte) ([]byte, error) {
	var j video.Video
	err := json.Unmarshal(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Video(ctx, j)
	return a.reply(res, err)
}

// doImages — job картинок из json
func (a *Engine) doImages(ctx context.Context, data []byte) ([]byte, error) {
	var j images.Job
	err := json.Unmarshal(data, &j)
	if err != nil {
		return a.malformed(data, "job is not json: "+err.Error())
	}

	res, err := a.Images(ctx, j)
	return a.reply(res, err)
}

// reply — результат в json и ошибка job как есть
func (a *Engine) reply(res any, err error) ([]byte, error) {
	data, jerr := json.Marshal(res)
	if jerr != nil {
		e := code.New(code.InternalTemporaryError, "result json", jerr)
		return nil, e
	}

	return data, err
}

// malformed — не job: результат с id (если читается) и INVALID_JOB
func (a *Engine) malformed(data []byte, message string) ([]byte, error) {
	var head struct {
		ID string `json:"id"`
	}
	json.Unmarshal(data, &head)

	e := code.New(code.InvalidJob, message, nil)
	res := struct {
		ID    string      `json:"id"`
		Error *code.Error `json:"error"`
	}{head.ID, e}

	out, _ := json.Marshal(res)
	return out, e
}

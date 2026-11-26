// Package beehttp is the Go client of bee over HTTP/JSON. One call is one
// job: it lasts as long as the job, so the context (or HTTP.Timeout) must
// allow the job timeout of bee.
//
//	c := &beehttp.Client{URL: "http://bee:8080", Token: os.Getenv("BEE_TOKEN")}
//
//	res, err := c.Images(ctx, images.Job{
//		ID:    "photo-1",
//		Input: presignedURL,
//		Tasks: []images.Task{{
//			ID:      "small",
//			Output:  "photos/1/small.webp",
//			Profile: images.Profile{Format: images.Webp, Quality: 80, MaxWidth: 320},
//		}},
//	})
//	if err != nil {
//		e := code.As(err) // e.Retryable: try again later
//	}
//
// A failed job returns its result with Error set and the same *code.Error
// as the error; a transport failure is INTERNAL_TEMPORARY_ERROR.
package beehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Client calls one bee over HTTP. HTTP nil is http.DefaultClient.
type Client struct {
	URL   string // http://bee:8080
	Token string // BEE_TOKEN
	HTTP  *http.Client

	names proto.Names
}

// Audio runs an audio job.
func (a *Client) Audio(ctx context.Context, j audio.Job) (audio.Result, error) {
	var res audio.Result
	err := a.call(ctx, proto.Audio, j.ID, j, &res)

	return res, err
}

// Stream runs an HLS job.
func (a *Client) Stream(ctx context.Context, j video.Stream) (video.Result, error) {
	var res video.Result
	err := a.call(ctx, proto.Stream, j.ID, j, &res)

	return res, err
}

// Video runs an MP4 job.
func (a *Client) Video(ctx context.Context, j video.Video) (video.Result, error) {
	var res video.Result
	err := a.call(ctx, proto.Video, j.ID, j, &res)

	return res, err
}

// Images runs an images job.
func (a *Client) Images(ctx context.Context, j images.Job) (images.Result, error) {
	var res images.Result
	err := a.call(ctx, proto.Images, j.ID, j, &res)

	return res, err
}

// call posts job to the path of kind and reads the result into res.
func (a *Client) call(ctx context.Context, kind proto.Kind, id string, job, res any) error {

	// the request
	body, err := json.Marshal(job)
	if err != nil {
		return a.fail(res, id, code.New(code.InvalidJob, "job json", err))
	}

	url := strings.TrimRight(a.URL, "/") + a.names.Path(kind)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return a.fail(res, id, code.New(code.InvalidJob, "request", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.Token)

	// the answer
	resp, err := a.client().Do(req)
	if err != nil {
		return a.fail(res, id, code.New(code.InternalTemporaryError, "bee http: "+err.Error(), err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return a.fail(res, id, code.New(code.InternalTemporaryError, "bee http: answer", err))
	}

	// the result and its error
	e := proto.ErrorOf(data)
	err = json.Unmarshal(data, res)

	switch {
	case err != nil:
		return a.fail(res, id, code.New(code.InternalTemporaryError, fmt.Sprintf("bee http %d: unreadable answer", resp.StatusCode), err))
	case e != nil:
		proto.Failure(res, id, e)
		return e
	case resp.StatusCode != http.StatusOK:
		return a.fail(res, id, code.New(code.InternalTemporaryError, fmt.Sprintf("bee http %d", resp.StatusCode), nil))
	}

	return nil
}

// fail fills res as a failure and returns e.
func (a *Client) fail(res any, id string, e *code.Error) error {
	proto.Failure(res, id, e)
	return e
}

// client is HTTP or the default client.
func (a *Client) client() *http.Client {
	if a.HTTP == nil {
		return http.DefaultClient
	}

	return a.HTTP
}

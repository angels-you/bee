// Package nats is the Go client of bee over NATS JetStream. A call
// publishes the job on its subject (the work queue every bee of the name
// shares) and waits for its result in the results stream; the job
// survives a restart of bee, which picks it up again.
//
//	c, err := nats.Dial("nats://nats:4222", os.Getenv("NATS_TOKEN"), proto.Names{})
//	if err != nil {
//		return err
//	}
//	defer c.Close()
//
//	res, err := c.Video(ctx, video.Video{
//		ID:      "clip-1",
//		Input:   presignedURL,
//		Output:  "clips/1/video.mp4",
//		Options: video.VerticalOptions(),
//		Profile: profile,
//	})
//
// proto.Names holds the subject bases of the bees (BEE_NATS_JOBS and
// BEE_NATS_DONE); the zero value is the defaults bee.jobs.new and
// bee.jobs.done. The context bounds the wait: a
// job whose wait ended still runs, and its result stays in the results
// stream for BEE_NATS_RESULT_TTL (Result reads it later). A failed job
// returns its result with Error set and the same *code.Error.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/angels-you/bee/pkg/tools"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Client sends jobs to the bees of one pair of subject bases.
type Client struct {
	nc    *natsgo.Conn
	js    jetstream.JetStream
	names proto.Names
	own   bool // the connection is the client's: Close closes it
}

// Dial connects to url with token (empty: none) for the bees of names.
func Dial(url, token string, names proto.Names, opts ...natsgo.Option) (*Client, error) {
	if token != "" {
		opts = append(opts, natsgo.Token(token))
	}

	nc, err := natsgo.Connect(url, opts...)
	if err != nil {
		return nil, err
	}

	a, err := New(nc, names)
	if err != nil {
		nc.Close()
		return nil, err
	}

	a.own = true
	return a, nil
}

// New uses an open connection for the bees of names.
func New(nc *natsgo.Conn, names proto.Names) (*Client, error) {
	err := names.Validate()
	if err != nil {
		return nil, err
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}

	return &Client{nc: nc, js: js, names: names}, nil
}

// Close closes the connection made by Dial.
func (a *Client) Close() {
	if a.own {
		a.nc.Close()
	}
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

// Result waits for the latest result of job id of kind already in the
// results stream (a job sent earlier) and reads it into res.
func (a *Client) Result(ctx context.Context, kind proto.Kind, id string, res any) error {
	if !tools.ID(id) {
		return a.fail(res, id, code.New(code.InvalidJob, "id must be 1..128 characters of A-Z, a-z, 0-9, _ and -", nil))
	}

	cons, err := a.results(ctx, kind, id, jetstream.DeliverLastPolicy)
	if err != nil {
		return a.fail(res, id, code.New(code.InternalTemporaryError, "bee nats: results", err))
	}

	return a.read(ctx, cons, id, res)
}

// call publishes job and waits for its result. The reader starts first
// (new results only), so no result is missed and no stale one is read.
func (a *Client) call(ctx context.Context, kind proto.Kind, id string, job, res any) error {
	// the id is part of the result subject
	if !tools.ID(id) {
		return a.fail(res, id, code.New(code.InvalidJob, "id must be 1..128 characters of A-Z, a-z, 0-9, _ and -", nil))
	}

	data, err := json.Marshal(job)
	if err != nil {
		return a.fail(res, id, code.New(code.InvalidJob, "job json", err))
	}

	// the reader of the result
	cons, err := a.results(ctx, kind, id, jetstream.DeliverNewPolicy)
	if err != nil {
		return a.fail(res, id, code.New(code.InternalTemporaryError, "bee nats: results", err))
	}

	// the job
	_, err = a.js.Publish(ctx, a.names.JobsSubject(kind), data)
	if err != nil {
		return a.fail(res, id, code.New(code.InternalTemporaryError, "bee nats: publish", err))
	}

	return a.read(ctx, cons, id, res)
}

// results is an ordered consumer of the result subject of job id.
func (a *Client) results(ctx context.Context, kind proto.Kind, id string, policy jetstream.DeliverPolicy) (jetstream.Consumer, error) {
	return a.js.OrderedConsumer(ctx, a.names.DoneStream(), jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{a.names.DoneSubject(kind, id)},
		DeliverPolicy:  policy,
	})
}

// read waits for one result until ctx is done.
func (a *Client) read(ctx context.Context, cons jetstream.Consumer, id string, res any) error {
	msg, err := cons.Next(jetstream.FetchContext(ctx))
	if err != nil {
		return a.fail(res, id, code.New(code.InternalTemporaryError, fmt.Sprintf("bee nats: no result: %v", err), err))
	}

	// the result and its error
	data := msg.Data()
	err = json.Unmarshal(data, res)
	if err != nil {
		return a.fail(res, id, code.New(code.InternalTemporaryError, "bee nats: unreadable result", err))
	}

	e := proto.ErrorOf(data)
	if e != nil {
		return e
	}

	return nil
}

// fail fills res as a failure and returns e.
func (a *Client) fail(res any, id string, e *code.Error) error {
	proto.Failure(res, id, e)
	return e
}

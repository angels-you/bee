// Package beenats is the Go client of bee over NATS JetStream: send and
// forget. A call checks the job and publishes it on the jobs subject; it
// returns once JetStream has accepted it. The result comes later on the
// done subject, where a worker of yours reads it with Done.
//
//	c, err := beenats.Dial("nats://nats:4222", os.Getenv("NATS_TOKEN"), proto.Names{})
//	if err != nil {
//		return err
//	}
//	defer c.Close()
//
//	// send
//	err = c.Images(ctx, images.Job{ID: "photo-1", Input: url, Tasks: tasks})
//
//	// a worker of yours: every result, until ctx ends
//	err = c.Done(ctx, "my-service", func(m proto.Envelope) error {
//		res, err := m.Images()
//		if err != nil {
//			return err
//		}
//		return save(m.ID, res) // nil: ack; an error: the result comes again later
//	})
//
// proto.Names holds the two subjects of the bees (BEE_NATS_JOBS and
// BEE_NATS_DONE); the zero value is the defaults bee.jobs and bee.done.
// The jobs survive a restart of bee; the results stay on the done stream
// for BEE_NATS_RESULT_TTL.
package beenats

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Client sends jobs to the bees of one pair of subjects and reads their
// results.
type Client struct {
	nc    *nats.Conn
	js    jetstream.JetStream
	names proto.Names
	own   bool          // the connection is the client's: Close closes it
	retry time.Duration // when a result whose handler failed comes again
}

// Dial connects to url with token (empty: none) for the bees of names.
func Dial(url, token string, names proto.Names, opts ...nats.Option) (*Client, error) {
	if token != "" {
		opts = append(opts, nats.Token(token))
	}

	// the connection
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, err
	}

	// the client
	a, err := New(nc, names)
	if err != nil {
		nc.Close()
		return nil, err
	}

	a.own = true
	return a, nil
}

// New uses an open connection for the bees of names.
func New(nc *nats.Conn, names proto.Names) (*Client, error) {
	err := names.Validate()
	if err != nil {
		return nil, err
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}

	a := &Client{nc: nc, js: js, names: names}
	a.init()

	return a, nil
}

// init sets the constant values of the client.
func (a *Client) init() {
	a.retry = 5 * time.Second
}

// Close closes the connection made by Dial.
func (a *Client) Close() {
	if a.own {
		a.nc.Close()
	}
}

// Audio sends an audio job.
func (a *Client) Audio(ctx context.Context, j audio.Job) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(ctx, proto.Audio, j)
}

// Stream sends an HLS job.
func (a *Client) Stream(ctx context.Context, j video.Stream) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(ctx, proto.Stream, j)
}

// Video sends an MP4 job.
func (a *Client) Video(ctx context.Context, j video.Video) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(ctx, proto.Video, j)
}

// Images sends an images job.
func (a *Client) Images(ctx context.Context, j images.Job) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(ctx, proto.Images, j)
}

// send publishes the envelope of job on the jobs subject and waits for
// JetStream to accept it.
func (a *Client) send(ctx context.Context, kind proto.Kind, job any) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}

	// the envelope
	msg, err := json.Marshal(proto.Envelope{Kind: kind, Job: data})
	if err != nil {
		return err
	}

	// the publish, confirmed by JetStream
	_, err = a.js.Publish(ctx, a.names.JobsSubject(), msg)
	return err
}

// Done reads the results with the durable consumer name until ctx ends
// (then it returns nil). Instances of a service with the same name share
// the results, every name gets its own copy. handle returning nil acks the
// result; an error makes it come again a few seconds later.
func (a *Client) Done(ctx context.Context, name string, handle func(proto.Envelope) error) error {
	if name == "" {
		return errors.New("beenats: the consumer name is required")
	}

	// the durable consumer of the results
	cons, err := a.js.CreateOrUpdateConsumer(ctx, a.names.DoneStream(), jetstream.ConsumerConfig{
		Durable:       name,
		FilterSubject: a.names.DoneSubject(),
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return err
	}

	// one result at a time until ctx ends
	for {
		msg, err := cons.Next(jetstream.FetchContext(ctx))
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, nats.ErrTimeout):
			continue
		case errors.Is(err, jetstream.ErrNoMessages):
			continue
		case err != nil:
			return err
		}

		a.handle(msg, handle)
	}
}

// handle gives one result to the handler: acked when it returns nil,
// nak'ed with the retry delay when it fails or the envelope is not json.
func (a *Client) handle(msg jetstream.Msg, handle func(proto.Envelope) error) {
	var m proto.Envelope
	err := json.Unmarshal(msg.Data(), &m)

	// not an envelope: it never will be
	if err != nil {
		msg.Term()
		return
	}

	err = handle(m)
	if err != nil {
		msg.NakWithDelay(a.retry)
		return
	}

	msg.Ack()
}

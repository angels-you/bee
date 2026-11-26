// Package beenats is the Go client of bee over NATS JetStream: send and
// forget. Audio, Stream, Video and Images check the job and publish it on
// the jobs subject; they return once JetStream has accepted it. The
// results come later on the done subject: SubscribeAudio, SubscribeStream,
// SubscribeVideo and SubscribeImages turn the client into a worker of the
// results.
//
//	c, err := beenats.Dial("nats://nats:4222", os.Getenv("NATS_TOKEN"), proto.Names{})
//	if err != nil {
//		return err
//	}
//	defer c.Close()
//
//	// the worker: every images result, until Close
//	err = c.SubscribeImages(func(res images.Result) {
//		if res.Error != nil {
//			log.Println(res.ID, res.Error.Code)
//			return
//		}
//		save(res.ID, res.Meta, res.Tasks)
//	})
//
//	// send
//	err = c.GenerateImages(images.Job{ID: 42, Input: url, Tasks: tasks, Meta: map[string]any{"user": 7}})
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
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/tools"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Client sends jobs to the bees of one pair of subjects and reads their
// results.
type Client struct {
	// Group is the group of the subscribers, "default" by default: every
	// instance of a service with the same group shares the results, a
	// service with another group gets its own copy. Set it before the
	// first Subscribe; A-Z, a-z, 0-9, _ and - only
	Group string

	nc      *nats.Conn
	js      jetstream.JetStream
	names   proto.Names
	own     bool          // the connection is the client's: Close closes it
	timeout time.Duration // the longest wait for JetStream to accept a job
	retry   time.Duration // when a result whose handler panicked comes again

	mu   sync.Mutex
	subs []jetstream.ConsumeContext
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

// init sets the defaults of the client.
func (a *Client) init() {
	a.Group = "default"
	a.timeout = 10 * time.Second
	a.retry = 5 * time.Second
}

// Close stops the subscribers and closes the connection made by Dial.
func (a *Client) Close() {
	a.mu.Lock()
	for _, sub := range a.subs {
		sub.Stop()
	}
	a.subs = nil
	a.mu.Unlock()

	if a.own {
		a.nc.Close()
	}
}

// GenerateAudio sends an audio job.
func (a *Client) GenerateAudio(j audio.Job) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(proto.Audio, j)
}

// GenerateStream sends an HLS job.
func (a *Client) GenerateStream(j video.StreamJob) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(proto.Stream, j)
}

// GenerateVideo sends an MP4 job.
func (a *Client) GenerateVideo(j video.VideoJob) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(proto.Video, j)
}

// GenerateImages sends an images job.
func (a *Client) GenerateImages(j images.Job) error {
	e := j.Validate()
	if e != nil {
		return e
	}

	return a.send(proto.Images, j)
}

// send publishes the envelope of job on the jobs subject and waits up to
// the timeout for JetStream to accept it.
func (a *Client) send(kind proto.Kind, job any) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()

	_, err = a.js.Publish(ctx, a.names.JobsSubject(), msg)
	return err
}

// SubscribeAudio calls v for every audio result, failed ones included
// (res.Error), until Close. It returns at once.
func (a *Client) SubscribeAudio(v func(res audio.Result)) error {
	return a.subscribe(proto.Audio, func(m proto.Envelope) error {
		res, err := m.Audio()
		if err != nil {
			return err
		}

		v(res)
		return nil
	})
}

// SubscribeStream calls v for every HLS result until Close.
func (a *Client) SubscribeStream(v func(res video.Result)) error {
	return a.subscribe(proto.Stream, func(m proto.Envelope) error {
		res, err := m.Video()
		if err != nil {
			return err
		}

		v(res)
		return nil
	})
}

// SubscribeVideo calls v for every MP4 result until Close.
func (a *Client) SubscribeVideo(v func(res video.Result)) error {
	return a.subscribe(proto.Video, func(m proto.Envelope) error {
		res, err := m.Video()
		if err != nil {
			return err
		}

		v(res)
		return nil
	})
}

// SubscribeImages calls v for every images result until Close.
func (a *Client) SubscribeImages(v func(res images.Result)) error {
	return a.subscribe(proto.Images, func(m proto.Envelope) error {
		res, err := m.Images()
		if err != nil {
			return err
		}

		v(res)
		return nil
	})
}

// subscribe runs the durable consumer of the group for the results of
// kind: results of other kinds are acked at once, the results of kind go
// to handle.
func (a *Client) subscribe(kind proto.Kind, handle func(proto.Envelope) error) error {
	name, err := a.names.Subscriber(a.Group, kind)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()

	// the durable consumer of the results
	cons, err := a.js.CreateOrUpdateConsumer(ctx, a.names.DoneStream(), jetstream.ConsumerConfig{
		Durable:       name,
		FilterSubject: a.names.DoneSubject(),
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("beenats: consumer %s: %w", name, err)
	}

	// the results, in the background until Close
	sub, err := cons.Consume(func(msg jetstream.Msg) {
		a.deliver(msg, kind, handle)
	})
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.subs = append(a.subs, sub)
	a.mu.Unlock()

	return nil
}

// deliver gives one result of kind to handle and acks it after; a result
// of another kind is acked at once; a message that is not a result is
// terminated; a panic of handle leaves the result for later.
func (a *Client) deliver(msg jetstream.Msg, kind proto.Kind, handle func(proto.Envelope) error) {
	var m proto.Envelope
	err := tools.Decode(msg.Data(), &m)

	switch {
	case err != nil:
		msg.Term()
		return
	case m.Kind != kind:
		msg.Ack()
		return
	}

	// the handler
	err = a.call(handle, m)
	switch {
	case errors.Is(err, errPanic{}):
		msg.NakWithDelay(a.retry)
	case err != nil:
		msg.Term()
	default:
		msg.Ack()
	}
}

// call runs handle; a panic of handle comes back as errPanic.
func (a *Client) call(handle func(proto.Envelope) error, m proto.Envelope) (err error) {
	defer func() {
		if recover() != nil {
			err = errPanic{}
		}
	}()

	return handle(m)
}

// errPanic is a result handler that panicked.
type errPanic struct{}

// Error is the text of errPanic.
func (e errPanic) Error() string {
	return "beenats: the result handler panicked"
}

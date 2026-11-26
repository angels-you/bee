// Package nats accepts jobs from NATS JetStream. BEE_NATS_JOBS and
// BEE_NATS_DONE are the two subjects (proto.Names): every job comes on the
// jobs subject as an envelope {"kind":"audio","job":{...}} through one
// durable pull consumer shared by every bee of the same subjects, so the
// instances share the queue; every result goes out on the done subject as
// {"kind":"audio","id":"...","result":{...}} into the results stream, kept
// BEE_NATS_RESULT_TTL, where the workers of the user read them. bee makes
// both streams and the consumer at start.
//
// A delivery ends one of three ways:
//
//	done or failure            the result is published (confirmed by JetStream), then ack
//	failure that redelivers    a code with Code.Redeliver: no result, nak with a delay doubling
//	                           from RetryDelay up to RetryMaxDelay; the delivery number
//	                           MaxDeliver is the last: its failure is published and acked
//	unfinished at shutdown     no result, nak at once: another bee takes it
//
// A job delivered again (a lost ack or result, a bee that died) runs
// again, writes over its keys and publishes its result again.
//
// A message that is not an envelope, has an unknown kind or a job without
// an id is terminated and logged; a readable job that is not valid is a
// failure (INVALID_JOB) on the done subject. While a job runs its message
// is kept alive (in progress every AckWait / 3).
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/angels-you/bee/pkg/tools"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/angels-you/bee/internal/config"
	"github.com/angels-you/bee/internal/engine"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/code"
)

// Consumer runs the jobs of every kind on one engine, BEE_CONCURRENCY at
// once.
type Consumer struct {
	cfg    config.NATS
	names  proto.Names
	log    *slog.Logger
	nc     *nats.Conn
	js     jetstream.JetStream
	cons   jetstream.Consumer
	engine *engine.Engine
	slots  chan struct{} // a slot is pulled for: never more jobs than the engine runs at once
	jobs   sync.WaitGroup
}

// Open connects (reconnecting forever after that), makes or updates the
// streams and the consumer, and returns the consumer, not yet
// pulling. worker names the connection.
func Open(ctx context.Context, cfg config.NATS, worker string, log *slog.Logger) (*Consumer, error) {
	// the connection, with the token when there is one
	opts := []nats.Option{nats.Name("bee " + worker), nats.MaxReconnects(-1)}
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	a := &Consumer{cfg: cfg, names: cfg.Names(), log: log, nc: nc}
	err = a.setup(ctx)
	if err != nil {
		nc.Close()
		return nil, err
	}

	return a, nil
}

// setup makes the job stream, the results stream and the shared consumer.
func (a *Consumer) setup(ctx context.Context) error {
	js, err := jetstream.New(a.nc)
	if err != nil {
		return fmt.Errorf("nats: jetstream: %w", err)
	}

	// file or memory
	storage := jetstream.FileStorage
	if a.cfg.Storage == "memory" {
		storage = jetstream.MemoryStorage
	}

	// the jobs: a work queue
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      a.names.JobsStream(),
		Subjects:  []string{a.names.JobsSubject()},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   storage,
		Replicas:  a.cfg.Replicas,
		MaxAge:    a.cfg.MaxAge,
	})
	if err != nil {
		return fmt.Errorf("nats: stream %s of subjects %s (another stream may hold them): %w", a.names.JobsStream(), a.names.JobsSubject(), err)
	}

	// the results: kept for the clients
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      a.names.DoneStream(),
		Subjects:  []string{a.names.DoneSubject()},
		Retention: jetstream.LimitsPolicy,
		Storage:   storage,
		Replicas:  a.cfg.Replicas,
		MaxAge:    a.cfg.ResultTTL,
	})
	if err != nil {
		return fmt.Errorf("nats: stream %s of subjects %s (another stream may hold them): %w", a.names.DoneStream(), a.names.DoneSubject(), err)
	}

	// the consumer of the jobs
	a.cons, err = js.CreateOrUpdateConsumer(ctx, a.names.JobsStream(), jetstream.ConsumerConfig{
		Durable:       a.names.Consumer(),
		FilterSubject: a.names.JobsSubject(),
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       a.cfg.AckWait,
		MaxDeliver:    -1,
	})
	if err != nil {
		return fmt.Errorf("nats: consumer %s: %w", a.names.Consumer(), err)
	}

	a.js = js
	return nil
}

// Ready reports the connection to NATS (readiness).
func (a *Consumer) Ready(ctx context.Context) error {
	s := a.nc.Status()
	if s != nats.CONNECTED {
		return fmt.Errorf("nats: %s", s)
	}

	return nil
}

// Run pulls jobs for e until ctx is done, then stops pulling and gives
// the running jobs shutdown to finish; those still running are cancelled
// and their messages nak'ed without a result (another bee takes them).
// Run closes the connection before it returns.
func (a *Consumer) Run(ctx context.Context, e *engine.Engine, concurrency int, shutdown time.Duration) {
	a.engine = e
	a.slots = make(chan struct{}, concurrency)
	work, cancel := context.WithCancel(context.Background())
	defer cancel()

	// pull until ctx is done
	a.log.Info("nats: pulling", "stream", a.names.JobsStream(), "subjects", a.names.JobsSubject(), "consumer", a.names.Consumer(), "concurrency", concurrency)
	for a.pull(ctx, work) {
	}

	// the running jobs get shutdown to finish
	done := make(chan struct{})
	go a.wait(done)

	t := time.NewTimer(shutdown)
	defer t.Stop()

	select {
	case <-done:
	case <-t.C:
		a.log.Warn("nats: shutdown timeout, cancelling the running jobs", "shutdown", shutdown.String())
		cancel()
		<-done
	}

	// the last acks and naks reach the server
	err := a.nc.Flush()
	if err != nil {
		a.log.Warn("nats: flush", "error", err.Error())
	}

	a.nc.Close()
	a.log.Info("nats: stopped")
}

// wait closes done when no job runs.
func (a *Consumer) wait(done chan<- struct{}) {
	a.jobs.Wait()
	close(done)
}

// pull takes one job once a slot is free and handles it in its own
// goroutine under work; false when ctx is done.
func (a *Consumer) pull(ctx, work context.Context) bool {
	select {
	case a.slots <- struct{}{}:
	case <-ctx.Done():
		return false
	}

	// the next message
	msg, err := a.cons.Next(jetstream.FetchContext(ctx))
	if err == nil {
		a.jobs.Add(1)
		go a.handle(work, msg)
		return true
	}

	<-a.slots
	switch {
	case ctx.Err() != nil:
		return false
	case errors.Is(err, nats.ErrTimeout):
		return true
	case errors.Is(err, jetstream.ErrNoMessages):
		return true
	}

	// NATS is away: no busy loop while it reconnects
	a.log.Warn("nats: pull", "error", err.Error())

	t := time.NewTimer(a.cfg.RetryDelay)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// handle runs one delivery: decode and run, then publish and ack, or nak.
func (a *Consumer) handle(work context.Context, msg jetstream.Msg) {
	defer a.jobs.Done()
	defer a.release()

	// not a JetStream delivery: nothing to report to
	meta, err := msg.Metadata()
	if err != nil {
		a.log.Error("nats: message without metadata", "error", err.Error())
		a.settle(msg.Term(), "term", "")
		return
	}

	// a redelivery is counted; the job runs again
	if meta.NumDelivered > 1 {
		a.engine.Metrics.Retried()
	}

	// the envelope: kind and id, else nothing to report to
	in, id, ok := a.open(msg)
	if !ok {
		return
	}

	// the job, kept alive while it runs
	stop := make(chan struct{})
	go a.keepAlive(msg, stop)
	res, err := a.engine.Do(work, in.Kind, in.Job)
	close(stop)

	// the end of the delivery
	e := code.As(err)
	switch {
	case err != nil && work.Err() != nil:
		a.log.Warn("nats: unfinished at shutdown, back to the queue", "job_id", tools.IDString(id), "delivered", meta.NumDelivered)
		a.settle(msg.Nak(), "nak", id)
	case err != nil && e.Code.Redeliver() && meta.NumDelivered < uint64(a.cfg.MaxDeliver):
		delay := a.backoff(meta.NumDelivered)
		a.log.Info("nats: retry later", "job_id", tools.IDString(id), "delivered", meta.NumDelivered, "error_code", string(e.Code), "delay_ms", delay.Milliseconds())
		a.settle(msg.NakWithDelay(delay), "nak", id)
	default:
		a.report(msg, proto.Envelope{Kind: in.Kind, ID: id, Result: res}, meta.NumDelivered)
	}
}

// open reads the envelope of a job and the id of its job. A message that
// is not an envelope, has an unknown kind or a job without a valid id is
// terminated and logged (ok false): there is nobody to report to. The
// data itself is never logged: it may hold a presigned url
func (a *Consumer) open(msg jetstream.Msg) (in proto.Envelope, id any, ok bool) {
	err := json.Unmarshal(msg.Data(), &in)

	// the id of the job inside
	var head struct {
		ID any `json:"id"`
	}
	if err == nil {
		tools.Decode(in.Job, &head)
	}

	switch {
	case err != nil:
		a.log.Warn("nats: not an envelope, terminated", "bytes", len(msg.Data()), "error", err.Error())
	case !in.Kind.Valid():
		a.log.Warn("nats: unknown job kind, terminated", "kind", string(in.Kind))
	case !tools.ID(head.ID):
		a.log.Warn("nats: job without a valid id, terminated", "kind", string(in.Kind))
	default:
		return in, head.ID, true
	}

	a.settle(msg.Term(), "term", "")
	return in, nil, false
}

// release frees the slot of a finished delivery.
func (a *Consumer) release() {
	<-a.slots
}

// report publishes the result envelope on the done subject, then acks. A
// publish JetStream does not confirm leaves the job to a redelivery: the
// result is never lost, at worst it is published twice
func (a *Consumer) report(msg jetstream.Msg, out proto.Envelope, delivered uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.AckWait)
	defer cancel()

	// the envelope
	data, err := json.Marshal(out)
	if err != nil {
		a.log.Error("nats: result json", "job_id", tools.IDString(out.ID), "error", err.Error())
		a.settle(msg.Term(), "term", out.ID)
		return
	}

	// the result
	_, err = a.js.Publish(ctx, a.names.DoneSubject(), data)
	if err != nil {
		a.log.Warn("nats: result not published, retry later", "job_id", tools.IDString(out.ID), "error", err.Error())
		a.settle(msg.NakWithDelay(a.backoff(delivered)), "nak", out.ID)
		return
	}

	// the ack
	a.settle(msg.DoubleAck(ctx), "ack", out.ID)
}

// settle logs a failed ack, nak or term: JetStream redelivers the
// message after AckWait.
func (a *Consumer) settle(err error, what string, id any) {
	if err != nil {
		a.log.Warn("nats: "+what+" failed, the message comes back after the ack wait", "job_id", tools.IDString(id), "error", err.Error())
	}
}

// keepAlive marks the message in progress every AckWait / 3 until stop
// is closed, so a long job keeps its delivery.
func (a *Consumer) keepAlive(msg jetstream.Msg, stop <-chan struct{}) {
	t := time.NewTicker(a.cfg.AckWait / 3)
	defer t.Stop()

	for {
		select {
		case <-stop:
			return
		case <-t.C:
			err := msg.InProgress()
			if err != nil {
				a.log.Warn("nats: in progress", "error", err.Error())
			}
		}
	}
}

// backoff is the nak delay after delivery n: RetryDelay × 2^(n−1), at
// most RetryMaxDelay.
func (a *Consumer) backoff(n uint64) time.Duration {
	d := a.cfg.RetryDelay
	for i := uint64(1); i < n && d < a.cfg.RetryMaxDelay; i++ {
		d *= 2
	}

	return min(d, a.cfg.RetryMaxDelay)
}

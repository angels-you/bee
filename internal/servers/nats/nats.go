// Package nats accepts jobs from NATS JetStream. Jobs come in on
// <name>.<kind>.new (the json of the job of kind) through one durable pull
// consumer shared by every bee of the name, so the instances share the
// queue; results (the json of the result) go out on
// <name>.<kind>.done.<id hash> into the results stream, kept
// BEE_NATS_RESULT_TTL, where a client reads the result of its job. bee
// makes both streams, the consumer and the ledger at start. Names come
// from proto.Names.
//
// A delivery ends one of three ways:
//
//	done or failure            the result is published (confirmed by JetStream), then ack
//	failure that redelivers    a code with Code.Redeliver: no result, nak with a delay doubling
//	                           from RetryDelay up to RetryMaxDelay; the delivery number
//	                           MaxDeliver is the last: its failure is published and acked
//	unfinished at shutdown     no result, nak at once: another bee takes it
//
// A job delivered again (a lost ack or result, a bee that died after
// publishing its output) finds its output stored, but the job saved its
// result in the ledger right before: it gets that result again instead of
// ALREADY_EXISTS.
//
// A message that is not a job is a failure (INVALID_JOB) when its id can
// be read, else it is terminated and logged. While a job runs its message
// is kept alive (in progress every AckWait / 3).
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	ledger *Ledger
	engine *engine.Engine
	slots  chan struct{} // a slot is pulled for: never more jobs than the engine runs at once
	jobs   sync.WaitGroup
}

// Open connects (reconnecting forever after that), makes or updates the
// streams, the consumer and the ledger, and returns the consumer, not yet
// pulling. worker names the connection.
func Open(ctx context.Context, cfg config.NATS, worker string, log *slog.Logger) (*Consumer, error) {
	nc, err := nats.Connect(cfg.URL, nats.Name("bee "+worker), nats.MaxReconnects(-1))
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	a := &Consumer{cfg: cfg, names: proto.Names{Name: cfg.Name}, log: log, nc: nc}
	err = a.setup(ctx)
	if err != nil {
		nc.Close()
		return nil, err
	}

	return a, nil
}

// setup makes the job stream, the results stream, the shared consumer and
// the ledger bucket.
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
		Name:      a.names.Stream(),
		Subjects:  []string{a.names.AllJobs()},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   storage,
		Replicas:  a.cfg.Replicas,
		MaxAge:    a.cfg.MaxAge,
	})
	if err != nil {
		return fmt.Errorf("nats: stream %s: %w", a.names.Stream(), err)
	}

	// the results: kept for the clients
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      a.names.Results(),
		Subjects:  []string{a.names.AllDone()},
		Retention: jetstream.LimitsPolicy,
		Storage:   storage,
		Replicas:  a.cfg.Replicas,
		MaxAge:    a.cfg.ResultTTL,
	})
	if err != nil {
		return fmt.Errorf("nats: stream %s: %w", a.names.Results(), err)
	}

	// the consumer of every job subject
	a.cons, err = js.CreateOrUpdateConsumer(ctx, a.names.Stream(), jetstream.ConsumerConfig{
		Durable:       a.names.Durable(),
		FilterSubject: a.names.AllJobs(),
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       a.cfg.AckWait,
		MaxDeliver:    -1,
	})
	if err != nil {
		return fmt.Errorf("nats: consumer %s: %w", a.names.Durable(), err)
	}

	// the ledger
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:   a.names.Ledger(),
		TTL:      a.cfg.ResultTTL,
		Storage:  storage,
		Replicas: a.cfg.Replicas,
	})
	if err != nil {
		return fmt.Errorf("nats: ledger %s: %w", a.names.Ledger(), err)
	}

	a.js = js
	a.ledger = &Ledger{kv: kv}
	return nil
}

// Ledger is the result ledger of the jobs, for engine.New.
func (a *Consumer) Ledger() *Ledger {
	return a.ledger
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
	a.log.Info("nats: pulling", "stream", a.names.Stream(), "subjects", a.names.AllJobs(), "consumer", a.names.Durable(), "concurrency", concurrency)
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

	if meta.NumDelivered > 1 {
		a.engine.Metrics.Retried()
	}

	// the kind of the job by its subject
	kind := a.kind(msg.Subject())
	if !kind.Valid() {
		a.malformed(msg, kind, "unknown subject "+msg.Subject())
		return
	}

	// the job, kept alive while it runs
	stop := make(chan struct{})
	go a.keepAlive(msg, stop)
	res, err := a.engine.Do(work, kind, msg.Data())
	close(stop)

	// the id of the result
	var head struct {
		ID string `json:"id"`
	}
	json.Unmarshal(res, &head)

	// the end of the delivery
	e := code.As(err)
	switch {
	case err != nil && work.Err() != nil:
		a.log.Warn("nats: unfinished at shutdown, back to the queue", "job_id", head.ID, "delivered", meta.NumDelivered)
		a.settle(msg.Nak(), "nak", head.ID)
	case err != nil && e.Code.Redeliver() && meta.NumDelivered < uint64(a.cfg.MaxDeliver):
		delay := a.backoff(meta.NumDelivered)
		a.log.Info("nats: retry later", "job_id", head.ID, "delivered", meta.NumDelivered, "error_code", string(e.Code), "delay_ms", delay.Milliseconds())
		a.settle(msg.NakWithDelay(delay), "nak", head.ID)
	default:
		a.report(msg, kind, head.ID, res, meta.NumDelivered)
	}
}

// kind is the job kind of a subject <name>.<kind>.new.
func (a *Consumer) kind(subject string) proto.Kind {
	rest := strings.TrimPrefix(subject, a.names.Durable()+".")
	kind, _, _ := strings.Cut(rest, ".")

	return proto.Kind(kind)
}

// release frees the slot of a finished delivery.
func (a *Consumer) release() {
	<-a.slots
}

// malformed handles a message that is not a job: a failure when its id
// can be read, else terminated. The data itself is never logged: it may
// hold a presigned url.
func (a *Consumer) malformed(msg jetstream.Msg, kind proto.Kind, cause string) {
	var head struct {
		ID string `json:"id"`
	}
	err := json.Unmarshal(msg.Data(), &head)

	switch {
	case err != nil, head.ID == "", !kind.Valid():
		a.log.Warn("nats: not a job, terminated", "bytes", len(msg.Data()), "error", cause)
		a.settle(msg.Term(), "term", "")
		return
	}

	// the failure goes to the results
	e := code.New(code.InvalidJob, cause, nil)
	res, _ := json.Marshal(struct {
		ID    string      `json:"id"`
		Error *code.Error `json:"error"`
	}{head.ID, e})

	a.log.Warn("nats: not a job", "job_id", head.ID, "error", cause)
	a.report(msg, kind, head.ID, res, 0)
}

// report publishes the result, then acks. A publish JetStream does not
// confirm leaves the job to a redelivery: the result is never lost, at
// worst it is published twice.
func (a *Consumer) report(msg jetstream.Msg, kind proto.Kind, id string, res []byte, delivered uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.AckWait)
	defer cancel()

	// the result
	_, err := a.js.Publish(ctx, a.names.Done(kind, id), res)
	if err != nil {
		a.log.Warn("nats: result not published, retry later", "job_id", id, "error", err.Error())
		a.settle(msg.NakWithDelay(a.backoff(delivered)), "nak", id)
		return
	}

	// the ack
	a.settle(msg.DoubleAck(ctx), "ack", id)
}

// settle logs a failed ack, nak or term: JetStream redelivers the
// message after AckWait.
func (a *Consumer) settle(err error, what, id string) {
	if err != nil {
		a.log.Warn("nats: "+what+" failed, the message comes back after the ack wait", "job_id", id, "error", err.Error())
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

// Ledger is engine.Ledger over the JetStream key-value bucket of the
// name: the result json of a done job under its key, kept
// BEE_NATS_RESULT_TTL. Shared by every bee of the name, so a redelivery to
// another instance finds it too.
type Ledger struct {
	kv jetstream.KeyValue
}

// Save stores value under key.
func (a *Ledger) Save(ctx context.Context, key string, value []byte) error {
	_, err := a.kv.Put(ctx, key, value)
	return err
}

// Load reads the value under key; false when there is none (never saved
// or expired).
func (a *Ledger) Load(ctx context.Context, key string) ([]byte, bool, error) {
	entry, err := a.kv.Get(ctx, key)
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}

	return entry.Value(), true, nil
}

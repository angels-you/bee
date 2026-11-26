package nats_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/angels-you/bee/internal/servers/nats"
	"github.com/angels-you/bee/internal/testmedia/testbee"
	"github.com/angels-you/bee/pkg/proto"
)

// TestConsumer — a job published on its subject, the result read from the
// results stream; an invalid job is a failure with its id, garbage is dropped.
func TestConsumer(t *testing.T) {
	bench := &testbee.Bench{T: t}
	bench.Start()

	// the consumer
	cfg := bench.Config.NATS
	cfg.URL = testbee.NATS(t, "")
	cfg.Storage = "memory"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cons, err := nats.Open(ctx, cfg, "test", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	go cons.Run(ctx, bench.Engine, 2, time.Second)

	// a raw publisher and a raw reader of the results
	nc, err := natsgo.Connect(cfg.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, _ := jetstream.New(nc)
	names := cfg.Names()

	results, err := js.OrderedConsumer(ctx, names.DoneStream(), jetstream.OrderedConsumerConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// a job, a job that is not valid, garbage and an unknown kind
	job, _ := json.Marshal(bench.Images("n1", "nats/1"))
	messages := []string{
		`{"kind":"images","job":` + string(job) + `}`,
		`{"kind":"audio","job":{"id":"n2","input":5}}`,
		`not json`,
		`{"kind":"photo","job":{"id":"n3"}}`,
		`{"kind":"audio","job":{"input":"x"}}`,
	}
	for _, m := range messages {
		_, err = js.Publish(ctx, names.JobsSubject(), []byte(m))
		if err != nil {
			t.Fatal(err)
		}
	}

	// two results: the done job and the invalid one; the rest is terminated
	got := map[string]proto.Envelope{}
	for range 2 {
		msg, err := results.Next(jetstream.FetchMaxWait(time.Minute))
		if err != nil {
			t.Fatalf("no result: %v (got %v)", err, got)
		}

		var m proto.Envelope
		json.Unmarshal(msg.Data(), &m)
		got[m.ID] = m
	}

	res, err := got["n1"].Images()
	switch {
	case err != nil:
		t.Errorf("n1: %v", err)
	case res.Error != nil, len(res.Tasks) != 2:
		t.Errorf("n1 result %+v", res)
	}

	bad := got["n2"]
	switch {
	case bad.Kind != proto.Audio:
		t.Errorf("n2 kind %q", bad.Kind)
	case bad.Failure() == nil:
		t.Error("n2 has no error")
	case bad.Failure().Code != "INVALID_JOB":
		t.Errorf("n2 code %s", bad.Failure().Code)
	}

	// nothing more: the garbage was terminated
	_, err = results.Next(jetstream.FetchMaxWait(2 * time.Second))
	if err == nil {
		t.Error("a result for a message that is not a job")
	}

	// ready
	err = cons.Ready(ctx)
	if err != nil {
		t.Errorf("ready: %v", err)
	}
}

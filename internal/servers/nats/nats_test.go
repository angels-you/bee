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
	"github.com/angels-you/bee/pkg/types/images"
)

// TestConsumer — a job published on its subject, the result read from the
// results stream; a message that is not a job is a failure with its id.
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

	// a raw publisher
	nc, err := natsgo.Connect(cfg.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, _ := jetstream.New(nc)
	names := cfg.Names()

	// a job
	job, _ := json.Marshal(bench.Images("n1", "nats/1"))
	_, err = js.Publish(ctx, names.JobsSubject(proto.Images), job)
	if err != nil {
		t.Fatal(err)
	}

	data := read(t, js, names, proto.Images, "n1")
	var res images.Result
	json.Unmarshal(data, &res)
	if res.Error != nil || len(res.Tasks) != 2 {
		t.Errorf("result %s", data)
	}

	// not a job, with an id
	_, err = js.Publish(ctx, names.JobsSubject(proto.Audio), []byte(`{"id":"n2","input":5}`))
	if err != nil {
		t.Fatal(err)
	}

	data = read(t, js, names, proto.Audio, "n2")
	json.Unmarshal(data, &res)
	if res.Error == nil || res.Error.Code != "INVALID_JOB" {
		t.Errorf("bad job result %s", data)
	}

	// ready
	err = cons.Ready(ctx)
	if err != nil {
		t.Errorf("ready: %v", err)
	}
}

// read waits for the result of job id of kind.
func read(t *testing.T, js jetstream.JetStream, names proto.Names, kind proto.Kind, id string) []byte {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cons, err := js.OrderedConsumer(ctx, names.DoneStream(), jetstream.OrderedConsumerConfig{FilterSubjects: []string{names.DoneSubject(kind, id)}})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := cons.Next(jetstream.FetchMaxWait(time.Minute))
	if err != nil {
		t.Fatalf("no result of %s: %v", id, err)
	}

	return msg.Data()
}

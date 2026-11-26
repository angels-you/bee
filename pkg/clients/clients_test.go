// End-to-end tests of the clients: client → server → engine → local
// storage, on every transport.
package clients_test

import (
	"context"
	"github.com/angels-you/bee/pkg/proto"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/angels-you/bee/internal/engine"
	grpcserver "github.com/angels-you/bee/internal/servers/grpc"
	httpserver "github.com/angels-you/bee/internal/servers/http"
	natsserver "github.com/angels-you/bee/internal/servers/nats"
	"github.com/angels-you/bee/internal/testmedia/testbee"
	"github.com/angels-you/bee/pkg/clients/grpc"
	"github.com/angels-you/bee/pkg/clients/http"
	"github.com/angels-you/bee/pkg/clients/nats"
	"github.com/angels-you/bee/pkg/storage/local"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// client is what every client of bee offers.
type client interface {
	Audio(ctx context.Context, j audio.Job) (audio.Result, error)
	Stream(ctx context.Context, j video.Stream) (video.Result, error)
	Video(ctx context.Context, j video.Video) (video.Result, error)
	Images(ctx context.Context, j images.Job) (images.Result, error)
}

// fixture is the bench, a local storage folder and a running server.
type fixture struct {
	t     *testing.T
	bench *testbee.Bench
	dir   string // the local storage
	ctx   context.Context
}

// init starts the bench sources and a context that ends with the test.
func (a *fixture) init(t *testing.T) {
	a.t = t
	a.bench = &testbee.Bench{T: t}
	a.bench.Start()
	a.dir = t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.ctx = ctx
}

// engine is an engine over the local storage.
func (a *fixture) engine() *engine.Engine {
	st, err := local.New(a.dir)
	if err != nil {
		a.t.Fatal(err)
	}

	e, err := engine.New(a.bench.Config, st, slog.New(slog.DiscardHandler))
	if err != nil {
		a.t.Fatal(err)
	}

	return e
}

// listen is a free local port.
func (a *fixture) listen() net.Listener {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.t.Fatal(err)
	}

	return lis
}

// stored checks that key is a file of the local storage.
func (a *fixture) stored(key string) {
	a.t.Helper()

	_, err := os.Stat(filepath.Join(a.dir, filepath.FromSlash(key)))
	if err != nil {
		a.t.Errorf("%s is not stored: %v", key, err)
	}
}

// run sends a job of every kind and a bad one through c.
func (a *fixture) run(c client) {
	t := a.t
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	defer cancel()

	// audio
	ar, err := c.Audio(ctx, a.bench.Audio("a1", "e2e/audio"))
	switch {
	case err != nil:
		t.Fatalf("audio: %v", err)
	case ar.ID != "a1":
		t.Errorf("audio id %q", ar.ID)
	case len(ar.Renditions) != 2:
		t.Errorf("audio renditions %+v", ar.Renditions)
	}
	a.stored("e2e/audio/master.m3u8")
	a.stored("e2e/audio/wave.json")

	// stream
	sr, err := c.Stream(ctx, a.bench.Stream("s1", "e2e/stream"))
	switch {
	case err != nil:
		t.Fatalf("stream: %v", err)
	case len(sr.Profiles) != 1:
		t.Errorf("stream profiles %+v", sr.Profiles)
	}
	a.stored("e2e/stream/index.m3u8")

	// video
	vr, err := c.Video(ctx, a.bench.Video("v1", "e2e/video"))
	switch {
	case err != nil:
		t.Fatalf("video: %v", err)
	case vr.Output != "e2e/video/video.mp4":
		t.Errorf("video output %q", vr.Output)
	}
	a.stored("e2e/video/video.mp4")

	// images
	ir, err := c.Images(ctx, a.bench.Images("i1", "e2e/images"))
	switch {
	case err != nil:
		t.Fatalf("images: %v", err)
	case len(ir.Tasks) != 2:
		t.Errorf("images tasks %+v", ir.Tasks)
	}
	a.stored("e2e/images/small.webp")

	// a failed job: the result and the error carry the code
	bad := a.bench.Images("i2", "e2e/bad")
	bad.Input = a.bench.URL("missing.tif")
	res, err := c.Images(ctx, bad)
	switch {
	case code.As(err) == nil:
		t.Fatal("missing source accepted")
	case code.As(err).Code != code.SourceUnavailable:
		t.Errorf("missing source: %v", err)
	case res.Error == nil:
		t.Error("no error in the result")
	case res.ID != "i2":
		t.Errorf("failed result id %q", res.ID)
	}

	// a rerun of a done job: it runs again over the same keys
	ar, err = c.Audio(ctx, a.bench.Audio("a1", "e2e/audio"))
	if err != nil || len(ar.Renditions) != 2 {
		t.Errorf("rerun: %+v %v", ar, err)
	}
}

// TestHTTP — the http client against the http server.
func TestHTTP(t *testing.T) {
	f := fixture{}
	f.init(t)

	lis := f.listen()
	srv := &httpserver.Server{Engine: f.engine(), Token: "test-token", Timeout: time.Minute}
	go srv.Serve(f.ctx, lis, time.Second)

	c := &beehttp.Client{URL: "http://" + lis.Addr().String(), Token: "test-token"}
	f.run(c)

	// a wrong token
	c.Token = "wrong"
	_, err := c.Images(f.ctx, f.bench.Images("t1", "e2e/token"))
	if code.As(err) == nil || code.As(err).Code != code.InvalidJob {
		t.Errorf("wrong token: %v", err)
	}
}

// TestGRPC — the grpc client against the grpc server.
func TestGRPC(t *testing.T) {
	f := fixture{}
	f.init(t)

	lis := f.listen()
	srv := &grpcserver.Server{Engine: f.engine(), Token: "test-token"}
	go srv.Serve(f.ctx, lis, time.Second)

	c, err := beegrpc.Dial(lis.Addr().String(), false, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f.run(c)

	// a wrong token
	wrong, err := beegrpc.Dial(lis.Addr().String(), false, "wrong")
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()

	_, err = wrong.Images(f.ctx, f.bench.Images("t1", "e2e/token"))
	if code.As(err) == nil || code.As(err).Code != code.InvalidJob {
		t.Errorf("wrong token: %v", err)
	}
}

// TestNATS — the nats client against the nats consumer and a nats-server
// of the test (skipped without nats-server): jobs are sent without
// waiting, a worker on Done gets the results of every kind, success and
// failure; two workers with one name share the results, another name gets
// its own copy.
func TestNATS(t *testing.T) {
	f := fixture{}
	f.init(t)

	// the consumer
	cfg := f.bench.Config.NATS
	cfg.Token = "nats-token"
	cfg.URL = testbee.NATS(t, cfg.Token)
	cfg.Storage = "memory"

	// fast redeliveries: a missing source redelivers MaxDeliver times
	cfg.RetryDelay = 100 * time.Millisecond
	cfg.RetryMaxDelay = 200 * time.Millisecond

	cons, err := natsserver.Open(f.ctx, cfg, "test", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	go cons.Run(f.ctx, f.engine(), 2, time.Second)

	c, err := beenats.Dial(cfg.URL, cfg.Token, cfg.Names())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// send and forget
	f.send(c)

	// a worker: all five results
	got := f.collect(c, "worker", 5)
	f.check(got)

	// two workers with one name share the results
	shared := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(f.ctx, time.Minute)
	defer cancel()

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Done(ctx, "shared", func(m proto.Envelope) error {
				mu.Lock()
				defer mu.Unlock()

				shared[m.ID]++
				if len(shared) == 5 {
					cancel()
				}
				return nil
			})
		}()
	}
	wg.Wait()

	for id, n := range shared {
		if n != 1 {
			t.Errorf("shared workers got %s %d times", id, n)
		}
	}
	if len(shared) != 5 {
		t.Errorf("shared workers got %d results", len(shared))
	}

	// another name: its own copy of every result
	other := f.collect(c, "other", 5)
	if len(other) != 5 {
		t.Errorf("another name got %d results", len(other))
	}
}

// send sends a job of every kind and one that fails; an invalid job is
// refused before it is sent.
func (a *fixture) send(c *beenats.Client) {
	t := a.t

	err := c.Audio(a.ctx, a.bench.Audio("a1", "nats/audio"))
	if err != nil {
		t.Fatalf("audio: %v", err)
	}

	err = c.Stream(a.ctx, a.bench.Stream("s1", "nats/stream"))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	err = c.Video(a.ctx, a.bench.Video("v1", "nats/video"))
	if err != nil {
		t.Fatalf("video: %v", err)
	}

	err = c.Images(a.ctx, a.bench.Images("i1", "nats/images"))
	if err != nil {
		t.Fatalf("images: %v", err)
	}

	// a job that fails in bee
	bad := a.bench.Images("i2", "nats/bad")
	bad.Input = a.bench.URL("missing.tif")
	err = c.Images(a.ctx, bad)
	if err != nil {
		t.Fatalf("bad images: %v", err)
	}

	// a job that is not valid is never sent
	err = c.Audio(a.ctx, a.bench.Audio("", "nats/none"))
	if code.As(err) == nil || code.As(err).Code != code.InvalidJob {
		t.Errorf("invalid job: %v", err)
	}
}

// collect reads n results with the consumer name.
func (a *fixture) collect(c *beenats.Client, name string, n int) map[string]proto.Envelope {
	got := map[string]proto.Envelope{}
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	defer cancel()

	err := c.Done(ctx, name, func(m proto.Envelope) error {
		got[m.ID] = m
		if len(got) == n {
			cancel()
		}
		return nil
	})
	if err != nil {
		a.t.Fatalf("done %s: %v", name, err)
	}

	return got
}

// check checks the five results of send.
func (a *fixture) check(got map[string]proto.Envelope) {
	t := a.t

	// audio
	ar, err := got["a1"].Audio()
	if err != nil || ar.Error != nil || len(ar.Renditions) != 2 {
		t.Errorf("audio: %+v %v", ar, err)
	}
	a.stored("nats/audio/master.m3u8")

	// stream and video
	sr, err := got["s1"].Video()
	if err != nil || sr.Error != nil || len(sr.Profiles) != 1 {
		t.Errorf("stream: %+v %v", sr, err)
	}

	vr, err := got["v1"].Video()
	if err != nil || vr.Error != nil || vr.Output != "nats/video/video.mp4" {
		t.Errorf("video: %+v %v", vr, err)
	}
	a.stored("nats/video/video.mp4")

	// images
	ir, err := got["i1"].Images()
	if err != nil || ir.Error != nil || len(ir.Tasks) != 2 {
		t.Errorf("images: %+v %v", ir, err)
	}

	// the failure
	e := got["i2"].Failure()
	if e == nil || e.Code != code.SourceUnavailable {
		t.Errorf("bad images: %v", e)
	}

	// a result of another kind is refused
	_, err = got["a1"].Images()
	if err == nil {
		t.Error("audio read as images")
	}
}

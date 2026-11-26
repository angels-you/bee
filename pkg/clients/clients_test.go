// End-to-end tests of the clients: client → server → engine → local
// storage, on every transport.
package clients_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angels-you/bee/internal/engine"
	grpcserver "github.com/angels-you/bee/internal/servers/grpc"
	httpserver "github.com/angels-you/bee/internal/servers/http"
	natsserver "github.com/angels-you/bee/internal/servers/nats"
	"github.com/angels-you/bee/internal/testmedia/testbee"
	grpcclient "github.com/angels-you/bee/pkg/clients/grpc"
	httpclient "github.com/angels-you/bee/pkg/clients/http"
	natsclient "github.com/angels-you/bee/pkg/clients/nats"
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

// engine is an engine over the local storage with ledger.
func (a *fixture) engine(ledger engine.Ledger) *engine.Engine {
	st, err := local.New(a.dir)
	if err != nil {
		a.t.Fatal(err)
	}

	e, err := engine.New(a.bench.Config, st, ledger, slog.New(slog.DiscardHandler))
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

	// a rerun of a done job: its result again
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
	srv := &httpserver.Server{Engine: f.engine(f.bench.Ledger), Token: "test-token", Timeout: time.Minute}
	go srv.Serve(f.ctx, lis, time.Second)

	c := &httpclient.Client{URL: "http://" + lis.Addr().String(), Token: "test-token"}
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
	srv := &grpcserver.Server{Engine: f.engine(f.bench.Ledger), Token: "test-token"}
	go srv.Serve(f.ctx, lis, time.Second)

	c, err := grpcclient.Dial(lis.Addr().String(), false, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f.run(c)

	// a wrong token
	wrong, err := grpcclient.Dial(lis.Addr().String(), false, "wrong")
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
// of the test (skipped without nats-server).
func TestNATS(t *testing.T) {
	f := fixture{}
	f.init(t)

	// the consumer and its ledger
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
	go cons.Run(f.ctx, f.engine(cons.Ledger()), 2, time.Second)

	c, err := natsclient.Dial(cfg.URL, cfg.Token, cfg.Names())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f.run(c)

	// the stored result of a job sent earlier
	var res images.Result
	err = c.Result(f.ctx, "images", "i1", &res)
	if err != nil || len(res.Tasks) != 2 {
		t.Errorf("stored result: %+v %v", res, err)
	}
}

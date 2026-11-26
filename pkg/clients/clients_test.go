// End-to-end tests of the clients: client → server → engine → local
// storage, on every transport.
package clients_test

import (
	"context"
	"encoding/json"
	"github.com/angels-you/bee/internal/config"
	"github.com/angels-you/bee/pkg/tools"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	GenerateAudio(ctx context.Context, j audio.Job) (audio.Result, error)
	GenerateStream(ctx context.Context, j video.StreamJob) (video.Result, error)
	GenerateVideo(ctx context.Context, j video.VideoJob) (video.Result, error)
	GenerateImages(ctx context.Context, j images.Job) (images.Result, error)
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
	ar, err := c.GenerateAudio(ctx, a.bench.Audio("a1", "e2e/audio"))
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
	sr, err := c.GenerateStream(ctx, a.bench.Stream("s1", "e2e/stream"))
	switch {
	case err != nil:
		t.Fatalf("stream: %v", err)
	case len(sr.Profiles) != 1:
		t.Errorf("stream profiles %+v", sr.Profiles)
	}
	a.stored("e2e/stream/index.m3u8")

	// video
	vr, err := c.GenerateVideo(ctx, a.bench.Video("v1", "e2e/video"))
	switch {
	case err != nil:
		t.Fatalf("video: %v", err)
	case vr.Output != "e2e/video/video.mp4":
		t.Errorf("video output %q", vr.Output)
	}
	a.stored("e2e/video/video.mp4")

	// images
	ir, err := c.GenerateImages(ctx, a.bench.Images("i1", "e2e/images"))
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
	bad.Meta = map[string]any{"user": "u1"}
	res, err := c.GenerateImages(ctx, bad)
	switch {
	case code.As(err) == nil:
		t.Fatal("missing source accepted")
	case code.As(err).Code != code.SourceUnavailable:
		t.Errorf("missing source: %v", err)
	case res.Error == nil:
		t.Error("no error in the result")
	case res.ID != "i2":
		t.Errorf("failed result id %v", res.ID)
	case res.Meta["user"] != "u1":
		t.Errorf("failed result meta %v", res.Meta)
	}

	// a rerun of a done job: it runs again over the same keys
	ar, err = c.GenerateAudio(ctx, a.bench.Audio("a1", "e2e/audio"))
	if err != nil || len(ar.Renditions) != 2 {
		t.Errorf("rerun: %+v %v", ar, err)
	}

	// a big number id and meta come back exactly as sent
	big := a.bench.Images("x", "e2e/big")
	big.ID = int64(9007199254740993)
	big.Meta = map[string]any{"user": 7}
	br, err := c.GenerateImages(ctx, big)
	if err != nil {
		t.Fatalf("big id: %v", err)
	}

	data, _ := json.Marshal(br)
	switch {
	case !strings.Contains(string(data), `"id":9007199254740993`):
		t.Errorf("big id changed: %s", data)
	case !strings.Contains(string(data), `"meta":{"user":7}`):
		t.Errorf("meta changed: %s", data)
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
	_, err := c.GenerateImages(f.ctx, f.bench.Images("t1", "e2e/token"))
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

	_, err = wrong.GenerateImages(f.ctx, f.bench.Images("t1", "e2e/token"))
	if code.As(err) == nil || code.As(err).Code != code.InvalidJob {
		t.Errorf("wrong token: %v", err)
	}
}

// TestNATS — the nats client against the nats consumer and a nats-server
// of the test (skipped without nats-server): jobs are sent without
// waiting, subscribers get the results of every kind, success and failure,
// with the id and the meta as sent; two clients of one group share the
// results, another group gets its own copy.
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

	// the worker: subscribers of every kind
	w := &worker{t: t, got: map[string]any{}, done: make(chan struct{})}
	w.want = 5
	c := w.dial(cfg, "worker")
	defer c.Close()

	// send and forget
	f.send(c)

	// all five results
	w.wait()
	f.check(w.got)

	// two clients of one group share the results, another group gets a copy
	shared := &worker{t: t, got: map[string]any{}, done: make(chan struct{}), want: 5}
	one := shared.dial(cfg, "shared")
	defer one.Close()
	two := shared.dial(cfg, "shared")
	defer two.Close()

	other := &worker{t: t, got: map[string]any{}, done: make(chan struct{}), want: 5}
	three := other.dial(cfg, "other")
	defer three.Close()

	shared.wait()
	other.wait()

	for id, n := range shared.count {
		if n != 1 {
			t.Errorf("the shared group got %s %d times", id, n)
		}
	}
}

// worker collects the results of every kind by id.
type worker struct {
	t     *testing.T
	mu    sync.Mutex
	got   map[string]any // the result by the id as text
	count map[string]int // how many times each id came
	want  int
	done  chan struct{}
}

// dial is a client of group with a subscriber of every kind.
func (a *worker) dial(cfg config.NATS, group string) *beenats.Client {
	c, err := beenats.Dial(group, cfg.URL, cfg.Token, cfg.Names())
	if err != nil {
		a.t.Fatal(err)
	}

	// one subscriber of every kind
	errs := []error{
		c.SubscribeAudio(func(res audio.Result) { a.add(res.ID, res) }),
		c.SubscribeStream(func(res video.Result) { a.add(res.ID, res) }),
		c.SubscribeVideo(func(res video.Result) { a.add(res.ID, res) }),
		c.SubscribeImages(func(res images.Result) { a.add(res.ID, res) }),
	}
	for _, err := range errs {
		if err != nil {
			a.t.Fatal(err)
		}
	}

	return c
}

// add records one result.
func (a *worker) add(id any, res any) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.count == nil {
		a.count = map[string]int{}
	}

	key := tools.IDString(id)
	a.got[key] = res
	a.count[key]++

	if len(a.got) == a.want {
		select {
		case <-a.done:
		default:
			close(a.done)
		}
	}
}

// wait waits for want results.
func (a *worker) wait() {
	select {
	case <-a.done:
	case <-time.After(3 * time.Minute):
		a.mu.Lock()
		defer a.mu.Unlock()
		a.t.Fatalf("got %d results of %d: %v", len(a.got), a.want, a.got)
	}
}

// send sends a job of every kind with numbers, strings and meta as ids,
// and one that fails; an invalid job is refused before it is sent.
func (a *fixture) send(c *beenats.Client) {
	t := a.t
	meta := map[string]any{"user": 7, "note": "from the test"}

	// audio with a big int id
	audioJob := a.bench.Audio("x", "nats/audio")
	audioJob.ID = int64(9007199254740993)
	audioJob.Meta = meta
	err := c.GenerateAudio(audioJob)
	if err != nil {
		t.Fatalf("audio: %v", err)
	}

	// stream with a string id
	stream := a.bench.Stream("abc", "nats/stream")
	stream.Meta = meta
	err = c.GenerateStream(stream)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	// video with a small number id
	videoJob := a.bench.Video("x", "nats/video")
	videoJob.ID = 3
	err = c.GenerateVideo(videoJob)
	if err != nil {
		t.Fatalf("video: %v", err)
	}

	// images with a text id with spaces
	err = c.GenerateImages(a.bench.Images("фото 4", "nats/images"))
	if err != nil {
		t.Fatalf("images: %v", err)
	}

	// a job that fails in bee
	bad := a.bench.Images("bad", "nats/bad")
	bad.Input = a.bench.URL("missing.tif")
	bad.Meta = meta
	err = c.GenerateImages(bad)
	if err != nil {
		t.Fatalf("bad images: %v", err)
	}

	// jobs that are not valid are never sent
	for _, id := range []any{nil, "", true, map[string]any{}, 1.5} {
		j := a.bench.Audio("x", "nats/none")
		j.ID = id
		err = c.GenerateAudio(j)
		if code.As(err) == nil || code.As(err).Code != code.InvalidJob {
			t.Errorf("invalid id %v: %v", id, err)
		}
	}
}

// check checks the five results of send: ids and meta as sent.
func (a *fixture) check(got map[string]any) {
	t := a.t

	// audio: the big id as the same number, the meta back
	ar, _ := got["9007199254740993"].(audio.Result)
	data, _ := json.Marshal(ar)
	switch {
	case ar.Error != nil, len(ar.Renditions) != 2:
		t.Errorf("audio: %+v", ar)
	case !strings.Contains(string(data), `"id":9007199254740993`):
		t.Errorf("audio id changed: %s", data)
	case !strings.Contains(string(data), `"meta":{"note":"from the test","user":7}`):
		t.Errorf("audio meta changed: %s", data)
	}
	a.stored("nats/audio/master.m3u8")

	// stream with the string id
	sr, _ := got["abc"].(video.Result)
	if sr.Error != nil || len(sr.Profiles) != 1 || sr.ID != "abc" || sr.Meta["user"] == nil {
		t.Errorf("stream: %+v", sr)
	}

	// video
	vr, _ := got["3"].(video.Result)
	if vr.Error != nil || vr.Output != "nats/video/video.mp4" {
		t.Errorf("video: %+v", vr)
	}
	a.stored("nats/video/video.mp4")

	// images
	ir, _ := got["фото 4"].(images.Result)
	if ir.Error != nil || len(ir.Tasks) != 2 {
		t.Errorf("images: %+v", ir)
	}

	// the failure, with its meta
	br, _ := got["bad"].(images.Result)
	if br.Error == nil || br.Error.Code != code.SourceUnavailable || br.Meta["note"] != "from the test" {
		t.Errorf("bad images: %+v", br)
	}
}

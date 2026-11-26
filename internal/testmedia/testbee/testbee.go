// Package testbee is the bee test bench: test config, in-memory storage, a
// source server, nats-server for the test duration and a ready engine
// with small sources of all three types
package testbee

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/angels-you/bee/internal/config"
	"github.com/angels-you/bee/internal/engine"
	"github.com/angels-you/bee/internal/testmedia/testaudio"
	"github.com/angels-you/bee/internal/testmedia/testimage"
	"github.com/angels-you/bee/internal/testmedia/testvideo"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Object is one saved upload
type Object struct {
	Data        []byte
	ContentType string
}

// Store is an in-memory storage.Storage; the zero value is empty and ready.
// A key in Fail fails its Put; Exists fails while ExistsErr is set
type Store struct {
	mu        sync.Mutex
	objects   map[string]Object
	puts      int
	Fail      map[string]error
	ExistsErr error
}

// Put stores the body under the key
func (a *Store) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	err := ctx.Err()
	if err != nil {
		return err
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.puts++
	err = a.Fail[key]
	switch {
	case err != nil:
		return err
	case int64(len(data)) != size:
		return errors.New("teststore: size does not match the body")
	}

	if a.objects == nil {
		a.objects = map[string]Object{}
	}
	a.objects[key] = Object{Data: data, ContentType: contentType}
	return nil
}

// Exists reports that the key is saved
func (a *Store) Exists(ctx context.Context, key string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ExistsErr != nil {
		return false, a.ExistsErr
	}

	_, ok := a.objects[key]
	return ok, nil
}

// Ping is always available
func (a *Store) Ping(ctx context.Context) error {
	return nil
}

// Keys returns all saved keys in order
func (a *Store) Keys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	keys := make([]string, 0, len(a.objects))
	for k := range a.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// Get returns the object by key
func (a *Store) Get(key string) (Object, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	o, ok := a.objects[key]
	return o, ok
}

// Puts returns the number of Put calls, failed ones included
func (a *Store) Puts() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.puts
}

// Sources is the test source server: files of Dir by name; a name under /hold/
// responds only once Hold is closed (or the request is canceled); FailNext
// makes the next requests for a name respond 503; requests are counted
type Sources struct {
	Dir  string
	Hold chan struct{}

	mu   sync.Mutex
	hits map[string]int
	fail map[string]int
}

// ServeHTTP serves one file
func (a *Sources) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)

	// count and maybe refuse
	a.mu.Lock()
	if a.hits == nil {
		a.hits = map[string]int{}
	}
	a.hits[name]++
	failing := a.fail[name] > 0
	if failing {
		a.fail[name]--
	}
	a.mu.Unlock()

	if failing {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// hold until released
	if strings.HasPrefix(r.URL.Path, "/hold/") {
		select {
		case <-a.Hold:
		case <-r.Context().Done():
			return
		}
	}

	http.ServeFile(w, r, filepath.Join(a.Dir, name))
}

// Count returns the number of requests for a name
func (a *Sources) Count(name string) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.hits[name]
}

// FailNext makes the next n requests for a name respond 503
func (a *Sources) FailNext(name string, n int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.fail == nil {
		a.fail = map[string]int{}
	}
	a.fail[name] = n
}

// Bench is the test engine: in-memory storage, a source server
// over generated files, logs in Logs. Start builds it;
// skips the test without ffmpeg, ffprobe or vips
type Bench struct {
	T       testing.TB
	Config  *config.Config
	Store   *Store
	Sources *Sources
	Server  *httptest.Server
	Logs    *bytes.Buffer
	Engine  *engine.Engine
}

// Start builds the bench with sources: song.wav (3 s), source.mp4 (2 s, 320×568
// with sound), photo.tif (320×240)
func (a *Bench) Start() {
	a.T.Helper()

	for _, bin := range []string{"ffmpeg", "ffprobe", "vips", "vipsheader"} {
		_, err := exec.LookPath(bin)
		if err != nil {
			a.T.Skip(bin + " is not installed")
		}
	}

	// sources
	dir := a.T.TempDir()
	testaudio.Generate(a.T, dir, testaudio.Opts{Name: "song.wav", Seconds: 3, VolumeDB: -12})
	media := testvideo.Maker{T: a.T, Dir: dir}
	media.Clip(testvideo.Clip{Name: "source.mp4", Width: 320, Height: 568, FPS: 30, Seconds: 2, Audio: true})
	photo := testimage.Photo(a.T, a.T.TempDir(), 320, 240)
	testimage.Vips(a.T, "copy", photo, filepath.Join(dir, "photo.tif"))

	a.Sources = &Sources{Dir: dir, Hold: make(chan struct{})}
	a.Server = httptest.NewServer(a.Sources)
	a.T.Cleanup(a.Server.Close)

	// engine
	a.Store = &Store{}
	a.Logs = &bytes.Buffer{}
	a.Config = a.config()
	a.Restart()
}

// Restart builds a new engine over the same bench (after editing Config)
func (a *Bench) Restart() {
	a.T.Helper()

	log := slog.New(slog.NewJSONHandler(a.Logs, nil))
	e, err := engine.New(a.Config, a.Store, log)
	if err != nil {
		a.T.Fatalf("engine: %v", err)
	}

	a.Engine = e
}

// config returns the bee defaults with fast, small test values
func (a *Bench) config() *config.Config {
	c := config.Defaults()

	c.WorkerID = "test-1"
	c.TempDir = a.T.TempDir()
	c.AllowInsecure = true
	c.DownloadTimeout = 30 * time.Second
	c.UploadConcurrency = 4
	c.Tools.Fpcalc = ""
	c.HTTP.Addr = "127.0.0.1:0"
	c.Token = "test-token"

	c.Audio.Timeout = 2 * time.Minute
	c.Audio.MinDuration = 500 * time.Millisecond
	c.Audio.Renditions = []int{64, 128}

	c.Video.Timeout = 2 * time.Minute
	c.Video.MinDuration = 500 * time.Millisecond
	c.Video.MaxDuration = time.Minute
	c.Video.MaxDimension = 1920
	c.Video.Threads = 1
	c.Video.SoundBitrate = 96
	c.Video.WatermarkWidth = 64

	c.Images.Timeout = 2 * time.Minute
	c.Images.Threads = 2

	return c
}

// URL returns the source url with a secret signature in the query
func (a *Bench) URL(name string) string {
	return a.Server.URL + "/" + name + "?X-Amz-Signature=secretsignature"
}

// Audio returns an audio job from song.wav with master at <dir>/master.m3u8 and waveform
func (a *Bench) Audio(id, dir string) audio.Job {
	return audio.Job{
		ID:       id,
		Input:    a.URL("song.wav"),
		Output:   dir + "/master.m3u8",
		WavePath: dir + "/wave.json",
	}
}

// Stream returns a small single-variant Stream from source.mp4 at <dir>/index.m3u8
func (a *Bench) Stream(id, dir string) video.StreamJob {
	return video.StreamJob{
		ID:       id,
		Input:    a.URL("source.mp4"),
		Output:   dir + "/index.m3u8",
		Options:  video.VerticalOptions(),
		Profiles: []video.Profile{{Name: "180p", Width: 180, Height: 320, FPS: 30, Preset: "ultrafast", VideoBitrate: 300}},
		HLS:      video.HLS{SegmentDuration: 1},
	}
}

// Video returns a small MP4 from source.mp4 at <dir>/video.mp4
func (a *Bench) Video(id, dir string) video.VideoJob {
	return video.VideoJob{
		ID:      id,
		Input:   a.URL("source.mp4"),
		Output:  dir + "/video.mp4",
		Options: video.VerticalOptions(),
		Profile: video.Profile{Width: 180, Height: 320, Preset: "ultrafast", VideoBitrate: 300},
	}
}

// Images returns two images from photo.tif in <dir>/
func (a *Bench) Images(id, dir string) images.Job {
	return images.Job{
		ID:    id,
		Input: a.URL("photo.tif"),
		Tasks: []images.Task{
			{ID: "small", Output: dir + "/small.webp", Profile: images.Profile{Format: images.Webp, Quality: 70, MaxWidth: 100}},
			{ID: "cover", Output: dir + "/cover.jpg", Profile: images.Profile{Format: images.Jpg, Fit: images.Cover, Quality: 80, MaxWidth: 64, MaxHeight: 64}},
		},
	}
}

// NATS starts nats-server with JetStream on a free port and temporary
// storage, stopped with the test; returns its url. A token turns on token
// auth. Without nats-server in PATH the test is skipped
func NATS(t testing.TB, token string) string {
	t.Helper()

	bin, err := exec.LookPath("nats-server")
	if err != nil {
		t.Skip("nats-server is not in PATH")
	}

	// free port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(lis.Addr().(*net.TCPAddr).Port)
	lis.Close()

	// server, stopped with the test
	args := []string{"-js", "-a", "127.0.0.1", "-p", port, "-sd", t.TempDir()}
	if token != "" {
		args = append(args, "--auth", token)
	}

	srv := &natsServer{cmd: exec.Command(bin, args...)}
	srv.cmd.Stdout = &srv.out
	srv.cmd.Stderr = &srv.out
	err = srv.cmd.Start()
	if err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	t.Cleanup(srv.stop)

	// up when it responds
	url := "nats://127.0.0.1:" + port
	for range 100 {
		nc, err := nats.Connect(url, nats.Token(token))
		if err == nil {
			nc.Close()
			return url
		}
		time.Sleep(50 * time.Millisecond)
	}

	srv.stop()
	t.Fatalf("%s did not start:\n%s", bin, srv.out.String())
	return ""
}

// natsServer is a running nats-server and its output
type natsServer struct {
	cmd  *exec.Cmd
	out  bytes.Buffer
	once sync.Once
}

// stop kills the server once and waits for it
func (a *natsServer) stop() {
	a.once.Do(a.kill)
}

// kill reports the process is done and its output complete
func (a *natsServer) kill() {
	a.cmd.Process.Kill()
	a.cmd.Wait()
}

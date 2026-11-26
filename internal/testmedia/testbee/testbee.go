// Package testbee — стенд тестов bee: конфиг тестов, хранилище и журнал в
// памяти, сервер оригиналов, nats-server на время теста и готовый движок
// с маленькими оригиналами всех трёх типов
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

// Object — одна сохранённая загрузка
type Object struct {
	Data        []byte
	ContentType string
}

// Store — storage.Storage в памяти; нулевое значение пустое и готово.
// Ключ в Fail роняет свой Put; Exists падает, пока задан ExistsErr
type Store struct {
	mu        sync.Mutex
	objects   map[string]Object
	puts      int
	Fail      map[string]error
	ExistsErr error
}

// Put — тело под ключом
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

// Exists — ключ сохранён
func (a *Store) Exists(ctx context.Context, key string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ExistsErr != nil {
		return false, a.ExistsErr
	}

	_, ok := a.objects[key]
	return ok, nil
}

// Ping — всегда доступно
func (a *Store) Ping(ctx context.Context) error {
	return nil
}

// Keys — все сохранённые ключи по порядку
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

// Get — объект по ключу
func (a *Store) Get(key string) (Object, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	o, ok := a.objects[key]
	return o, ok
}

// Puts — вызовов Put, вместе с неудачными
func (a *Store) Puts() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.puts
}

// Ledger — engine.Ledger в памяти; нулевое значение пустое и готово
type Ledger struct {
	mu      sync.Mutex
	results map[string][]byte
}

// Save — значение под ключом
func (a *Ledger) Save(ctx context.Context, key string, value []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.results == nil {
		a.results = map[string][]byte{}
	}
	a.results[key] = value
	return nil
}

// Load — значение по ключу
func (a *Ledger) Load(ctx context.Context, key string) ([]byte, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	value, ok := a.results[key]
	return value, ok, nil
}

// Sources — сервер оригиналов тестов: файлы Dir по имени; имя под /hold/
// отвечает, только когда закрыт Hold (или запрос отменён); FailNext
// заставляет следующие запросы имени отвечать 503; запросы считаются
type Sources struct {
	Dir  string
	Hold chan struct{}

	mu   sync.Mutex
	hits map[string]int
	fail map[string]int
}

// ServeHTTP — один файл
func (a *Sources) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)

	// счёт и, может быть, отказ
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

	// держим до отпускания
	if strings.HasPrefix(r.URL.Path, "/hold/") {
		select {
		case <-a.Hold:
		case <-r.Context().Done():
			return
		}
	}

	http.ServeFile(w, r, filepath.Join(a.Dir, name))
}

// Count — запросов имени
func (a *Sources) Count(name string) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.hits[name]
}

// FailNext — следующие n запросов имени отвечают 503
func (a *Sources) FailNext(name string, n int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.fail == nil {
		a.fail = map[string]int{}
	}
	a.fail[name] = n
}

// Bench — движок тестов: хранилище и журнал в памяти, сервер оригиналов
// над сгенерированными файлами, логи в Logs. Start его собирает;
// пропускает тест без ffmpeg, ffprobe или vips
type Bench struct {
	T       testing.TB
	Config  *config.Config
	Store   *Store
	Ledger  *Ledger
	Sources *Sources
	Server  *httptest.Server
	Logs    *bytes.Buffer
	Engine  *engine.Engine
}

// Start — стенд с оригиналами: song.wav (3 с), source.mp4 (2 с, 320×568
// со звуком), photo.tif (320×240)
func (a *Bench) Start() {
	a.T.Helper()

	for _, bin := range []string{"ffmpeg", "ffprobe", "vips", "vipsheader"} {
		_, err := exec.LookPath(bin)
		if err != nil {
			a.T.Skip(bin + " is not installed")
		}
	}

	// оригиналы
	dir := a.T.TempDir()
	testaudio.Generate(a.T, dir, testaudio.Opts{Name: "song.wav", Seconds: 3, VolumeDB: -12})
	media := testvideo.Maker{T: a.T, Dir: dir}
	media.Clip(testvideo.Clip{Name: "source.mp4", Width: 320, Height: 568, FPS: 30, Seconds: 2, Audio: true})
	photo := testimage.Photo(a.T, a.T.TempDir(), 320, 240)
	testimage.Vips(a.T, "copy", photo, filepath.Join(dir, "photo.tif"))

	a.Sources = &Sources{Dir: dir, Hold: make(chan struct{})}
	a.Server = httptest.NewServer(a.Sources)
	a.T.Cleanup(a.Server.Close)

	// движок
	a.Store = &Store{}
	a.Ledger = &Ledger{}
	a.Logs = &bytes.Buffer{}
	a.Config = a.config()
	a.Restart()
}

// Restart — новый движок над тем же стендом (после правки Config)
func (a *Bench) Restart() {
	a.T.Helper()

	log := slog.New(slog.NewJSONHandler(a.Logs, nil))
	e, err := engine.New(a.Config, a.Store, a.Ledger, log)
	if err != nil {
		a.T.Fatalf("engine: %v", err)
	}

	a.Engine = e
}

// config — дефолты bee с быстрыми и маленькими значениями тестов
func (a *Bench) config() *config.Config {
	c := config.Defaults()

	c.WorkerID = "test-1"
	c.TempDir = a.T.TempDir()
	c.AllowInsecure = true
	c.DownloadTimeout = 30 * time.Second
	c.UploadConcurrency = 4
	c.Tools.Fpcalc = ""
	c.HTTP.Addr = "127.0.0.1:0"

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

// URL — url оригинала с секретной подписью в query
func (a *Bench) URL(name string) string {
	return a.Server.URL + "/" + name + "?X-Amz-Signature=secretsignature"
}

// Audio — аудио job из song.wav с master в <dir>/master.m3u8 и waveform
func (a *Bench) Audio(id, dir string) audio.Job {
	return audio.Job{
		ID:     id,
		Input:  a.URL("song.wav"),
		Output: dir + "/master.m3u8",
		Wave:   dir + "/wave.json",
	}
}

// Stream — маленький Stream одного варианта из source.mp4 в <dir>/index.m3u8
func (a *Bench) Stream(id, dir string) video.Stream {
	return video.Stream{
		ID:       id,
		Input:    a.URL("source.mp4"),
		Output:   dir + "/index.m3u8",
		Options:  video.VerticalOptions(),
		Profiles: []video.Profile{{Name: "180p", Width: 180, Height: 320, FPS: 30, Preset: "ultrafast", VideoBitrate: 300}},
		HLS:      video.HLS{SegmentDuration: 1},
	}
}

// Video — маленький MP4 из source.mp4 в <dir>/video.mp4
func (a *Bench) Video(id, dir string) video.Video {
	return video.Video{
		ID:      id,
		Input:   a.URL("source.mp4"),
		Output:  dir + "/video.mp4",
		Options: video.VerticalOptions(),
		Profile: video.Profile{Width: 180, Height: 320, Preset: "ultrafast", VideoBitrate: 300},
	}
}

// Images — две картинки из photo.tif в <dir>/
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

// NATS — nats-server с JetStream на свободном порту и временном
// хранилище, останавливается с тестом; его url. Без nats-server в PATH
// тест пропускается
func NATS(t testing.TB) string {
	t.Helper()

	bin, err := exec.LookPath("nats-server")
	if err != nil {
		t.Skip("nats-server is not in PATH")
	}

	// свободный порт
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(lis.Addr().(*net.TCPAddr).Port)
	lis.Close()

	// сервер, останавливается с тестом
	srv := &natsServer{cmd: exec.Command(bin, "-js", "-a", "127.0.0.1", "-p", port, "-sd", t.TempDir())}
	srv.cmd.Stdout = &srv.out
	srv.cmd.Stderr = &srv.out
	err = srv.cmd.Start()
	if err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	t.Cleanup(srv.stop)

	// поднят, когда отвечает
	url := "nats://127.0.0.1:" + port
	for range 100 {
		nc, err := nats.Connect(url)
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

// natsServer — запущенный nats-server и его вывод
type natsServer struct {
	cmd  *exec.Cmd
	out  bytes.Buffer
	once sync.Once
}

// stop — убивает сервер один раз и ждёт его
func (a *natsServer) stop() {
	a.once.Do(a.kill)
}

// kill — процесс завершён, его вывод полный
func (a *natsServer) kill() {
	a.cmd.Process.Kill()
	a.cmd.Wait()
}

package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angels-you/bee/internal/testmedia/testbee"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// fixture — стенд одного теста
type fixture struct {
	t     *testing.T
	bench *testbee.Bench
}

// init — стенд с движком
func (a *fixture) init(t *testing.T) {
	a.t = t
	a.bench = &testbee.Bench{T: t}
	a.bench.Start()
}

// codeOf — код ошибки, пусто если это не *code.Error
func (a *fixture) codeOf(err error) code.Code {
	var e *code.Error
	if errors.As(err, &e) {
		return e.Code
	}

	return ""
}

// has — объект есть в хранилище
func (a *fixture) has(key string) bool {
	_, ok := a.bench.Store.Get(key)
	return ok
}

// noSecret — логи без подписи url
func (a *fixture) noSecret() {
	if strings.Contains(a.bench.Logs.String(), "secretsignature") {
		a.t.Error("the presigned signature is in the logs")
	}
}

// TestAudio — HLS двух вариантов, waveform и json, master последним;
// параметры job поверх дефолтов
func TestAudio(t *testing.T) {
	f := fixture{}
	f.init(t)
	ctx := context.Background()

	j := f.bench.Audio("a1", "tracks/1")
	j.Meta = "tracks/1/meta.json"
	j.Analysis = "tracks/1/analysis.json"

	res, err := f.bench.Engine.Audio(ctx, j)
	if err != nil {
		t.Fatal(err)
	}

	// результат
	switch {
	case res.ID != "a1":
		t.Errorf("id %q", res.ID)
	case len(res.Renditions) != 2:
		t.Fatalf("renditions %+v", res.Renditions)
	case res.Renditions[0].Folder != "64":
		t.Errorf("first rendition %s", res.Renditions[0].Folder)
	case len(res.Waveform) != 512:
		t.Errorf("waveform %d points", len(res.Waveform))
	case res.SHA256 == "":
		t.Error("no sha256")
	case res.Size == 0:
		t.Error("no size")
	case res.Encrypted:
		t.Error("plain job is encrypted")
	case res.Meta == nil:
		t.Error("no metadata")
	case res.Loudness == nil:
		t.Error("no loudness")
	}

	// объекты
	for _, key := range []string{"tracks/1/master.m3u8", "tracks/1/64/index.m3u8", "tracks/1/128/init.mp4", "tracks/1/wave.json", "tracks/1/meta.json", "tracks/1/analysis.json"} {
		if !f.has(key) {
			t.Errorf("%s is missing: %v", key, f.bench.Store.Keys())
		}
	}

	master, _ := f.bench.Store.Get("tracks/1/master.m3u8")
	if master.ContentType != "application/vnd.apple.mpegurl" {
		t.Errorf("master content type %q", master.ContentType)
	}

	// параметры job: один вариант, своя длина waveform
	j = f.bench.Audio("a2", "tracks/2")
	j.Options.Renditions = []int{96}
	j.Options.WaveformPoints = 100
	j.Options.Segment = 2

	res, err = f.bench.Engine.Audio(ctx, j)
	switch {
	case err != nil:
		t.Fatal(err)
	case len(res.Renditions) != 1:
		t.Errorf("renditions %+v", res.Renditions)
	case res.Renditions[0].Folder != "96":
		t.Errorf("rendition %s", res.Renditions[0].Folder)
	case len(res.Waveform) != 100:
		t.Errorf("waveform %d points", len(res.Waveform))
	}

	f.noSecret()
}

// TestAudioEncrypted — сегменты зашифрованы, в media playlist строки ключа
// с KeyURI job, в master ключа нет
func TestAudioEncrypted(t *testing.T) {
	f := fixture{}
	f.init(t)

	j := f.bench.Audio("e1", "enc")
	j.Key = []byte("0123456789abcdef")
	j.KeyURI = "skd://e1"

	res, err := f.bench.Engine.Audio(context.Background(), j)
	switch {
	case err != nil:
		t.Fatal(err)
	case !res.Encrypted:
		t.Error("not encrypted")
	}

	media, _ := f.bench.Store.Get("enc/64/index.m3u8")
	if !bytes.Contains(media.Data, []byte(`#EXT-X-KEY:METHOD=AES-128,URI="skd://e1",IV=0x`)) {
		t.Errorf("no key line:\n%s", media.Data)
	}

	master, _ := f.bench.Store.Get("enc/master.m3u8")
	if bytes.Contains(master.Data, []byte("EXT-X-KEY")) {
		t.Error("the master has a key")
	}
}

// TestRerun — повтор той же job отдаёт сохранённый результат, чужая job
// на тех же ключах — ALREADY_EXISTS
func TestRerun(t *testing.T) {
	f := fixture{}
	f.init(t)
	ctx := context.Background()

	// аудио
	first, err := f.bench.Engine.Audio(ctx, f.bench.Audio("r1", "rerun/a"))
	if err != nil {
		t.Fatal(err)
	}

	again, err := f.bench.Engine.Audio(ctx, f.bench.Audio("r1", "rerun/a"))
	switch {
	case err != nil:
		t.Fatalf("rerun: %v", err)
	case again.SHA256 != first.SHA256:
		t.Error("rerun result differs")
	}

	_, err = f.bench.Engine.Audio(ctx, f.bench.Audio("other", "rerun/a"))
	if f.codeOf(err) != code.AlreadyExists {
		t.Errorf("other job: %v", err)
	}

	// видео
	_, err = f.bench.Engine.Video(ctx, f.bench.Video("v1", "rerun/v"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.bench.Engine.Video(ctx, f.bench.Video("v1", "rerun/v"))
	if err != nil {
		t.Errorf("video rerun: %v", err)
	}

	_, err = f.bench.Engine.Video(ctx, f.bench.Video("v2", "rerun/v"))
	if f.codeOf(err) != code.AlreadyExists {
		t.Errorf("other video job: %v", err)
	}
}

// TestStream — HLS одного варианта с превью-картинкой, в результате
// размеры и сегменты
func TestStream(t *testing.T) {
	f := fixture{}
	f.init(t)

	j := f.bench.Stream("s1", "video/1")
	j.Thumbnails = []video.Thumbnail{{Output: "video/1/cover.jpg"}}
	j.Options.Normalize = true

	res, err := f.bench.Engine.Stream(context.Background(), j)
	switch {
	case err != nil:
		t.Fatal(err)
	case res.Output != "video/1/index.m3u8":
		t.Errorf("output %q", res.Output)
	case len(res.Profiles) != 1:
		t.Fatalf("profiles %+v", res.Profiles)
	case res.Profiles[0].Width != 180:
		t.Errorf("width %d", res.Profiles[0].Width)
	case res.Profiles[0].Segments == 0:
		t.Error("no segments")
	case len(res.Thumbnails) != 1:
		t.Errorf("thumbnails %+v", res.Thumbnails)
	case res.SHA256 == "":
		t.Error("no sha256")
	}

	for _, key := range []string{"video/1/index.m3u8", "video/1/init.mp4", "video/1/cover.jpg"} {
		if !f.has(key) {
			t.Errorf("%s is missing: %v", key, f.bench.Store.Keys())
		}
	}

	f.noSecret()
}

// TestStreamEncrypted — два варианта, master и ключи в media playlist
func TestStreamEncrypted(t *testing.T) {
	f := fixture{}
	f.init(t)

	j := f.bench.Stream("s2", "video/2")
	j.Profiles = append(j.Profiles, video.Profile{Name: "144p", Width: 144, Height: 256, FPS: 30, Preset: "ultrafast", VideoBitrate: 200})
	j.HLS.Key = []byte("0123456789abcdef")
	j.HLS.KeyURI = "skd://2"

	_, err := f.bench.Engine.Stream(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}

	media, _ := f.bench.Store.Get("video/2/180p/index.m3u8")
	if !bytes.Contains(media.Data, []byte(`URI="skd://2"`)) {
		t.Errorf("no key line:\n%s", media.Data)
	}

	master, _ := f.bench.Store.Get("video/2/index.m3u8")
	if !bytes.Contains(master.Data, []byte("144p/index.m3u8")) {
		t.Errorf("master:\n%s", master.Data)
	}
}

// TestVideo — один MP4
func TestVideo(t *testing.T) {
	f := fixture{}
	f.init(t)

	res, err := f.bench.Engine.Video(context.Background(), f.bench.Video("v1", "video/3"))
	switch {
	case err != nil:
		t.Fatal(err)
	case res.Output != "video/3/video.mp4":
		t.Errorf("output %q", res.Output)
	case res.Size == 0:
		t.Error("no size")
	}

	mp4, _ := f.bench.Store.Get("video/3/video.mp4")
	if mp4.ContentType != "video/mp4" {
		t.Errorf("content type %q", mp4.ContentType)
	}
}

// TestImages — каждый результат с размерами, Content-Type по формату
func TestImages(t *testing.T) {
	f := fixture{}
	f.init(t)

	res, err := f.bench.Engine.Images(context.Background(), f.bench.Images("i1", "img/1"))
	switch {
	case err != nil:
		t.Fatal(err)
	case len(res.Tasks) != 2:
		t.Fatalf("tasks %+v", res.Tasks)
	case res.Tasks[0].Width != 100:
		t.Errorf("small width %d", res.Tasks[0].Width)
	case res.Tasks[1].Width != 64:
		t.Errorf("cover width %d", res.Tasks[1].Width)
	case res.SourceWidth != 320:
		t.Errorf("source width %d", res.SourceWidth)
	}

	small, _ := f.bench.Store.Get("img/1/small.webp")
	if small.ContentType != "image/webp" {
		t.Errorf("content type %q", small.ContentType)
	}

	// avif с усилием из job
	effort := 0
	j := f.bench.Images("i2", "img/2")
	j.Tasks = []images.Task{{ID: "a", Output: "img/2/a.avif", Profile: images.Profile{Format: images.Avif, Quality: 50, MaxWidth: 64, Effort: &effort}}}
	_, err = f.bench.Engine.Images(context.Background(), j)
	if err != nil {
		t.Errorf("avif: %v", err)
	}

	// лимит коробки из конфига
	j = f.bench.Images("i3", "img/3")
	j.Tasks[0].Profile.MaxWidth = 9000
	_, err = f.bench.Engine.Images(context.Background(), j)
	if f.codeOf(err) != code.InvalidDimensions {
		t.Errorf("box above the limit: %v", err)
	}

	f.noSecret()
}

// TestSourceErrors — ошибки оригинала с кодами и без подписи в логах
func TestSourceErrors(t *testing.T) {
	f := fixture{}
	f.init(t)
	ctx := context.Background()

	// нет файла
	j := f.bench.Images("x1", "err/1")
	j.Input = f.bench.URL("missing.tif")
	_, err := f.bench.Engine.Images(ctx, j)
	if f.codeOf(err) != code.SourceUnavailable {
		t.Errorf("missing source: %v", err)
	}

	// не аудио
	a := f.bench.Audio("x2", "err/2")
	a.Input = f.bench.URL("photo.tif")
	_, err = f.bench.Engine.Audio(ctx, a)
	if f.codeOf(err) != code.InvalidAudio {
		t.Errorf("not audio: %v", err)
	}

	// не видео
	v := f.bench.Video("x3", "err/3")
	v.Input = f.bench.URL("song.wav")
	_, err = f.bench.Engine.Video(ctx, v)
	if f.codeOf(err) != code.InvalidVideo {
		t.Errorf("not video: %v", err)
	}

	// слишком длинное аудио
	f.bench.Config.Audio.MaxDuration = time.Second
	f.bench.Restart()
	_, err = f.bench.Engine.Audio(ctx, f.bench.Audio("x4", "err/4"))
	if f.codeOf(err) != code.DurationLimitExceeded {
		t.Errorf("too long: %v", err)
	}

	// плохая job до слота
	_, err = f.bench.Engine.Audio(ctx, f.bench.Audio("", "err/5"))
	if f.codeOf(err) != code.InvalidJob {
		t.Errorf("no id: %v", err)
	}

	f.noSecret()
}

// TestUploadFailure — упавшая загрузка не публикует вход; повтор
// проходит
func TestUploadFailure(t *testing.T) {
	f := fixture{}
	f.init(t)
	ctx := context.Background()

	f.bench.Store.Fail = map[string]error{"up/64/init.mp4": errors.New("disk full")}
	_, err := f.bench.Engine.Audio(ctx, f.bench.Audio("u1", "up"))
	switch {
	case f.codeOf(err) != code.StorageUnavailable:
		t.Errorf("failed upload: %v", err)
	case f.has("up/master.m3u8"):
		t.Error("master published after a failed upload")
	}

	f.bench.Store.Fail = nil
	_, err = f.bench.Engine.Audio(ctx, f.bench.Audio("u1", "up"))
	if err != nil {
		t.Errorf("retry: %v", err)
	}
}

// TestBusy — без свободного слота и без ожидания — временная ошибка
func TestBusy(t *testing.T) {
	f := fixture{}
	f.init(t)

	f.bench.Config.Concurrency = 1
	f.bench.Restart()

	// первая job держит слот на скачивании
	held := f.bench.Images("b1", "busy/1")
	held.Input = f.bench.Server.URL + "/hold/photo.tif"
	done := make(chan error, 1)
	go func() {
		_, err := f.bench.Engine.Images(context.Background(), held)
		done <- err
	}()

	// ждём, пока первая займёт слот
	for range 100 {
		if f.bench.Sources.Count("photo.tif") > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	_, err := f.bench.Engine.Images(context.Background(), f.bench.Images("b2", "busy/2"))
	if f.codeOf(err) != code.InternalTemporaryError {
		t.Errorf("busy: %v", err)
	}

	close(f.bench.Sources.Hold)
	err = <-done
	if err != nil {
		t.Errorf("held job: %v", err)
	}
}

// TestCancel — отменённая job — временная ошибка
func TestCancel(t *testing.T) {
	f := fixture{}
	f.init(t)

	ctx, cancel := context.WithCancel(context.Background())
	j := f.bench.Images("c1", "cancel")
	j.Input = f.bench.Server.URL + "/hold/photo.tif"

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := f.bench.Engine.Images(ctx, j)
	if f.codeOf(err) != code.InternalTemporaryError {
		t.Errorf("cancelled: %v", err)
	}
}

// TestMetrics — счётчики по типам и кодам в формате Prometheus
func TestMetrics(t *testing.T) {
	f := fixture{}
	f.init(t)
	ctx := context.Background()

	_, err := f.bench.Engine.Images(ctx, f.bench.Images("m1", "m/1"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.bench.Engine.Audio(ctx, f.bench.Audio("", "m/2"))
	if err == nil {
		t.Fatal("bad job accepted")
	}

	var out bytes.Buffer
	f.bench.Engine.Metrics.WritePrometheus(&out)

	want := []string{
		`bee_jobs_total{type="images"} 1`,
		`bee_jobs_completed_total{type="images"} 1`,
		`bee_jobs_failed_total{type="audio",code="INVALID_JOB"} 1`,
		`bee_jobs_active 0`,
		`bee_job_duration_seconds_count{type="images"} 1`,
	}
	for _, line := range want {
		if !strings.Contains(out.String(), line) {
			t.Errorf("no %s in:\n%s", line, out.String())
		}
	}

	// ready
	err = f.bench.Engine.Ready(ctx)
	if err != nil {
		t.Errorf("ready: %v", err)
	}
}

// TestResultJSON — результат упавшей job сериализуется без причины
func TestResultJSON(t *testing.T) {
	f := fixture{}
	f.init(t)

	res, _ := f.bench.Engine.Images(context.Background(), images.Job{ID: "j"})
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), `"code":"INVALID_JOB"`) {
		t.Errorf("result %s", data)
	}
}

package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixture is the test env: all BEE_* are removed before each test and
// restored afterwards
type fixture struct {
	t     *testing.T
	saved map[string]string
}

// init clears env of BEE_*
func (a *fixture) init(t *testing.T) {
	a.t = t
	a.saved = map[string]string{}

	for _, kv := range os.Environ() {
		key, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "BEE_") {
			a.saved[key] = value
			os.Unsetenv(key)
		}
	}

	t.Cleanup(a.restore)
}

// restore removes the test BEE_* (including those from .env) and brings back the previous ones
func (a *fixture) restore() {
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "BEE_") {
			os.Unsetenv(key)
		}
	}

	for key, value := range a.saved {
		os.Setenv(key, value)
	}
}

// set sets the test env keys
func (a *fixture) set(kv ...string) {
	for i := 0; i+1 < len(kv); i += 2 {
		a.t.Setenv(kv[i], kv[i+1])
	}
}

// minimal returns only the required keys: local storage and http
func (a *fixture) minimal() {
	a.set(
		"BEE_STORAGE", "local",
		"BEE_LOCAL_DIR", a.t.TempDir(),
		"BEE_HTTP_ADDR", ":8080",
		"BEE_TOKEN", "secret",
	)
}

// TestDefaults checks that without optional keys everything comes from defaults
func TestDefaults(t *testing.T) {
	f := fixture{}
	f.init(t)
	f.minimal()

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	switch {
	case c.WorkerID != "bee-1":
		t.Errorf("worker id %s", c.WorkerID)
	case c.Concurrency != 2:
		t.Errorf("concurrency %d", c.Concurrency)
	case c.DownloadTimeout != 2*time.Minute:
		t.Errorf("download timeout %s", c.DownloadTimeout)
	case !reflect.DeepEqual(c.Audio.Renditions, []int{128, 64, 256}):
		t.Errorf("renditions %v", c.Audio.Renditions)
	case c.Audio.LUFS != -14:
		t.Errorf("audio lufs %v", c.Audio.LUFS)
	case c.Video.MaxDimension != 3840:
		t.Errorf("video max dimension %d", c.Video.MaxDimension)
	case c.Images.AvifEffort != 4:
		t.Errorf("avif effort %d", c.Images.AvifEffort)
	case c.NATS.Enabled():
		t.Error("nats must be off")
	case c.GRPC.Enabled():
		t.Error("grpc must be off")
	case !c.HTTP.Enabled():
		t.Error("http must be on")
	case !c.Storage.S3.Secure:
		t.Error("s3 must be https by default")
	}

	// storage opens
	st, err := c.Storage.Open()
	if err != nil || st == nil {
		t.Errorf("open local: %v", err)
	}
}

// TestOverride checks that env overrides defaults, including to zero and false
func TestOverride(t *testing.T) {
	f := fixture{}
	f.init(t)
	f.minimal()
	f.set(
		"BEE_QUEUE_WAIT", "5s",
		"BEE_SOURCE_HOSTS", "r2.cloudflarestorage.com,example.com",
		"BEE_AUDIO_RENDITIONS", "96,192",
		"BEE_AUDIO_ANALYSIS", "true",
		"BEE_IMAGES_AVIF_EFFORT", "0",
		"BEE_S3_SECURE", "false",
		"BEE_NATS_URL", "nats://localhost:4222",
	)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	switch {
	case c.QueueWait != 5*time.Second:
		t.Errorf("queue wait %s", c.QueueWait)
	case len(c.SourceHosts) != 2:
		t.Errorf("source hosts %v", c.SourceHosts)
	case !reflect.DeepEqual(c.Audio.Renditions, []int{96, 192}):
		t.Errorf("renditions %v", c.Audio.Renditions)
	case !c.Audio.Analysis:
		t.Error("analysis must be on")
	case c.Images.AvifEffort != 0:
		t.Errorf("avif effort %d", c.Images.AvifEffort)
	case c.Storage.S3.Secure:
		t.Error("s3 secure must be off")
	case !c.NATS.Enabled():
		t.Error("nats must be on")
	}
}

// TestRequired checks that bee does not start without storage and transport, and the error
// names both
func TestRequired(t *testing.T) {
	f := fixture{}
	f.init(t)

	_, err := Load()
	switch {
	case err == nil:
		t.Fatal("empty env must be refused")
	case !strings.Contains(err.Error(), "BEE_STORAGE is required"):
		t.Errorf("no storage error: %v", err)
	case !strings.Contains(err.Error(), "no transport"):
		t.Errorf("no transport error: %v", err)
	}

	// s3 requires only its own keys
	f.set(
		"BEE_STORAGE", "s3",
		"BEE_S3_PROVIDER", "nope",
		"BEE_GRPC_ADDR", ":9090",
		"BEE_TOKEN", "secret",
	)

	_, err = Load()
	want := []string{"BEE_S3_PROVIDER", "BEE_S3_KEY", "BEE_S3_SECRET", "BEE_S3_BUCKET"}
	for _, key := range want {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s is not named: %v", key, err)
		}
	}

	if strings.Contains(err.Error(), "BEE_LOCAL_DIR") {
		t.Errorf("local keys are not required with s3: %v", err)
	}
}

// TestInvalid checks that all out-of-range and unparsable values are named
func TestInvalid(t *testing.T) {
	f := fixture{}
	f.init(t)
	f.minimal()
	f.set(
		"BEE_CONCURRENCY", "0",
		"BEE_AUDIO_RENDITIONS", "128,128",
		"BEE_AUDIO_LUFS", "3",
		"BEE_VIDEO_TRUE_PEAK", "-20",
		"BEE_IMAGES_TIMEOUT", "1m",
		"BEE_NATS_URL", "nats://localhost:4222",
		"BEE_NATS_STORAGE", "disk",
	)

	_, err := Load()
	if err == nil {
		t.Fatal("invalid values must be refused")
	}

	want := []string{
		"BEE_CONCURRENCY",
		"BEE_AUDIO_RENDITIONS: 128 is used twice",
		"BEE_AUDIO_LUFS",
		"BEE_VIDEO_TRUE_PEAK",
		"at most BEE_IMAGES_TIMEOUT",
		"BEE_NATS_STORAGE",
	}
	for _, text := range want {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("%q is not named: %v", text, err)
		}
	}

	// not a number
	f.set("BEE_CONCURRENCY", "many")

	_, err = Load()
	if err == nil {
		t.Error("unparsable value must be refused")
	}
}

// TestEnvFile checks that cmd/.env matches defaults: the example shows every key
// with its default
func TestEnvFile(t *testing.T) {
	f := fixture{}
	f.init(t)

	c, err := Load("../../cmd/.env")
	if err != nil {
		t.Fatal(err)
	}

	// what the example sets beyond the defaults
	want := &Config{}
	want.defaults()
	want.Storage.Kind = "local"
	want.Storage.Local.Dir = "/data/out"
	want.HTTP.Addr = ":8080"
	want.Token = "change-me"

	// an empty list from the file and nil from defaults are the same
	if len(c.SourceHosts) == 0 {
		c.SourceHosts = nil
	}

	if !reflect.DeepEqual(c, want) {
		t.Errorf("cmd/.env differs from defaults:\n%+v\n%+v", c, want)
	}
}

// TestSourceHostAllowed checks exact host and subdomains; an empty list allows any
func TestSourceHostAllowed(t *testing.T) {
	c := &Config{}

	if !c.SourceHostAllowed("any.com") {
		t.Error("empty list must allow any host")
	}

	c.SourceHosts = []string{"r2.cloudflarestorage.com", "Example.com"}
	cases := map[string]bool{
		"r2.cloudflarestorage.com":     true,
		"acc.r2.cloudflarestorage.com": true,
		"EXAMPLE.com":                  true,
		"cdn.example.com":              true,
		"evilexample.com":              false,
		"example.com.evil.net":         false,
	}

	for host, want := range cases {
		if c.SourceHostAllowed(host) != want {
			t.Errorf("%s: want %v", host, want)
		}
	}
}

// TestToken — http and grpc never run without a token; nats alone needs
// none; the health server always has an address.
func TestToken(t *testing.T) {
	f := fixture{}
	f.init(t)
	f.set(
		"BEE_STORAGE", "local",
		"BEE_LOCAL_DIR", t.TempDir(),
		"BEE_GRPC_ADDR", ":9090",
	)

	// grpc without a token
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BEE_TOKEN is required") {
		t.Errorf("grpc without a token: %v", err)
	}

	// nats alone
	os.Unsetenv("BEE_GRPC_ADDR")
	f.set("BEE_NATS_URL", "nats://localhost:4222")

	c, err := Load()
	switch {
	case err != nil:
		t.Fatalf("nats without a token: %v", err)
	case c.Health.Addr != ":8081":
		t.Errorf("health addr %q", c.Health.Addr)
	}

	// bad subjects
	bad := map[string][2]string{
		"wildcard": {"bee.jobs.*", "bee.done"},
		"equal":    {"bee.jobs", "bee.jobs"},
		"inside":   {"bee.jobs", "bee.jobs.done"},
		"empty":    {"bee..jobs", "bee.done"},
	}
	for name, bases := range bad {
		f.set("BEE_NATS_JOBS", bases[0], "BEE_NATS_DONE", bases[1])

		_, err = Load()
		if err == nil || !strings.Contains(err.Error(), "BEE_NATS_JOBS") {
			t.Errorf("%s: %v", name, err)
		}
	}

	// other valid subjects
	f.set("BEE_NATS_JOBS", "bee.hetzner.jobs", "BEE_NATS_DONE", "bee.hetzner.done")

	c, err = Load()
	switch {
	case err != nil:
		t.Fatal(err)
	case c.NATS.Names().JobsStream() != "BEE_HETZNER_JOBS":
		t.Errorf("jobs stream %s", c.NATS.Names().JobsStream())
	}
}

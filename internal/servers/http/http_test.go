package http_test

import (
	"bytes"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angels-you/bee/internal/servers/health"
	"github.com/angels-you/bee/internal/servers/http"
	"github.com/angels-you/bee/internal/testmedia/testbee"
	"github.com/angels-you/bee/pkg/types/images"
)

// fixture is the bench and both servers of one test.
type fixture struct {
	t      *testing.T
	bench  *testbee.Bench
	jobs   *httptest.Server
	health *httptest.Server
}

// init starts the bench, the job server and the health server.
func (a *fixture) init(t *testing.T) {
	a.t = t
	a.bench = &testbee.Bench{T: t}
	a.bench.Start()

	jobs := &http.Server{Engine: a.bench.Engine, Token: "test-token"}
	a.jobs = httptest.NewServer(jobs.Handler())
	t.Cleanup(a.jobs.Close)

	probe := &health.Server{Engine: a.bench.Engine}
	a.health = httptest.NewServer(probe.Handler())
	t.Cleanup(a.health.Close)
}

// post sends body to path with token; the status and the body.
func (a *fixture) post(path, token string, body []byte) (int, []byte) {
	req, err := nethttp.NewRequest(nethttp.MethodPost, a.jobs.URL+path, bytes.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// get reads path of the health server.
func (a *fixture) get(path string) (int, string) {
	resp, err := nethttp.Get(a.health.URL + path)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

// TestJobs — the token, a done job, a failed job with its status.
func TestJobs(t *testing.T) {
	f := fixture{}
	f.init(t)

	job, _ := json.Marshal(f.bench.Images("h1", "http/1"))

	// no token, a wrong token
	status, _ := f.post("/v1/images", "", job)
	if status != nethttp.StatusUnauthorized {
		t.Errorf("no token: %d", status)
	}

	status, _ = f.post("/v1/images", "wrong", job)
	if status != nethttp.StatusUnauthorized {
		t.Errorf("wrong token: %d", status)
	}

	// a done job
	status, body := f.post("/v1/images", "test-token", job)
	var res images.Result
	json.Unmarshal(body, &res)
	switch {
	case status != nethttp.StatusOK:
		t.Fatalf("job: %d %s", status, body)
	case len(res.Tasks) != 2:
		t.Errorf("result %s", body)
	}

	// a bad job
	status, body = f.post("/v1/images", "test-token", []byte(`{"id":"bad"}`))
	if status != nethttp.StatusBadRequest || !strings.Contains(string(body), "INVALID_JOB") {
		t.Errorf("bad job: %d %s", status, body)
	}

	// not json
	status, _ = f.post("/v1/audio", "test-token", []byte(`nope`))
	if status != nethttp.StatusBadRequest {
		t.Errorf("not json: %d", status)
	}

	// no health on the job server
	resp, err := nethttp.Get(f.jobs.URL + "/health")
	if err == nil && resp.StatusCode == nethttp.StatusOK {
		t.Error("health on the job server")
	}
}

// TestHealth — health, ready and metrics without a token.
func TestHealth(t *testing.T) {
	f := fixture{}
	f.init(t)

	status, body := f.get("/health")
	if status != nethttp.StatusOK {
		t.Errorf("health: %d %s", status, body)
	}

	status, body = f.get("/ready")
	if status != nethttp.StatusOK {
		t.Errorf("ready: %d %s", status, body)
	}

	status, body = f.get("/metrics")
	if status != nethttp.StatusOK || !strings.Contains(body, "bee_jobs_active") {
		t.Errorf("metrics: %d %s", status, body)
	}
}

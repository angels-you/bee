// Package health is the small HTTP server every bee runs on
// BEE_HEALTH_ADDR, whatever transports are on, without a token:
//
//	GET /health   liveness: the process answers
//	GET /ready    readiness: tools, storage and every connected transport
//	GET /metrics  Prometheus text format
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/angels-you/bee/internal/engine"
)

// Probe is a dependency that is ready or not (a transport).
type Probe interface {
	Ready(ctx context.Context) error
}

// Server answers health, readiness and metrics of one engine.
type Server struct {
	Engine *engine.Engine
	Probes []Probe // asked by /ready besides the engine
}

// Handler is the router.
func (a *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /ready", a.ready)
	mux.HandleFunc("GET /metrics", a.metrics)

	return mux
}

// health answers GET /health: the process is alive.
func (a *Server) health(w http.ResponseWriter, r *http.Request) {
	a.reply(w, []byte(`{"ok":true}`), http.StatusOK)
}

// ready answers GET /ready: the tools answer, the storage pings, every
// probe is ready; else 503 with the first error.
func (a *Server) ready(w http.ResponseWriter, r *http.Request) {
	err := a.Engine.Ready(r.Context())
	for _, p := range a.Probes {
		if err == nil {
			err = p.Ready(r.Context())
		}
	}

	if err != nil {
		body, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
		a.reply(w, body, http.StatusServiceUnavailable)
		return
	}

	a.reply(w, []byte(`{"ok":true}`), http.StatusOK)
}

// metrics answers GET /metrics in the Prometheus text format.
func (a *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	a.Engine.Metrics.WritePrometheus(w)
}

// reply writes json with status.
func (a *Server) reply(w http.ResponseWriter, body []byte, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

// Serve listens on lis until ctx is done, then stops within shutdown.
func (a *Server) Serve(ctx context.Context, lis net.Listener, shutdown time.Duration) error {
	srv := &http.Server{
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	// the server
	errc := make(chan error, 1)
	go a.listen(srv, lis, errc)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// the stop
	stop, cancel := context.WithTimeout(context.Background(), shutdown)
	defer cancel()

	err := srv.Shutdown(stop)
	if errors.Is(err, context.DeadlineExceeded) {
		return srv.Close()
	}

	return err
}

// listen runs Serve into errc.
func (a *Server) listen(srv *http.Server, lis net.Listener, errc chan<- error) {
	err := srv.Serve(lis)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	errc <- err
}

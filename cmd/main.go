// Command bee runs the bee worker: it reads the BEE_* environment, opens
// the storage, starts the engine, the health server and the transports
// the environment turns on, and stops gracefully on SIGTERM or SIGINT.
//
//	bee           run the worker
//	bee health    exit 0 when the health server of a running bee answers (docker HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/angels-you/bee/internal/config"
	"github.com/angels-you/bee/internal/engine"
	grpcserver "github.com/angels-you/bee/internal/servers/grpc"
	"github.com/angels-you/bee/internal/servers/health"
	httpserver "github.com/angels-you/bee/internal/servers/http"
	natsserver "github.com/angels-you/bee/internal/servers/nats"
	"github.com/angels-you/bee/pkg/storage"
)

func main() {
	a := &app{}

	// the health check of the docker image
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "health":
			os.Exit(a.health())
		}
	}

	os.Exit(a.run())
}

// app is one run of bee.
type app struct {
	cfg    *config.Config
	log    *slog.Logger
	store  storage.Storage
	engine *engine.Engine
	nats   *natsserver.Consumer
	probes []health.Probe
	wg     sync.WaitGroup
	errc   chan error // the first server that stops on its own
}

// run starts bee and waits for a signal; the exit code.
func (a *app) run() int {
	a.log = slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// the config: every problem at once
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee: the configuration is not valid:\n  %s\n", strings.ReplaceAll(err.Error(), "\n", "\n  "))
		return 1
	}
	a.cfg = cfg

	// the context of the run: ends on SIGTERM or SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// the storage, the transports, the engine
	err = a.start(ctx)
	if err != nil {
		a.log.Error("bee: start failed", "error", err.Error())
		return 1
	}

	// until a signal or a server failure
	a.errc = make(chan error, 4)
	a.serve(ctx)

	code := 0
	select {
	case <-ctx.Done():
		a.log.Info("bee: stopping", "shutdown", cfg.ShutdownTimeout.String())
	case err = <-a.errc:
		a.log.Error("bee: a server failed", "error", err.Error())
		code = 1
		stop()
	}

	// the servers finish their jobs
	a.wg.Wait()
	a.log.Info("bee: stopped")
	return code
}

// start opens the storage, the engine and the nats consumer, and logs
// what is on.
func (a *app) start(ctx context.Context) error {
	cfg := a.cfg

	// the storage
	st, err := cfg.Storage.Open()
	if err != nil {
		return fmt.Errorf("storage %s: %w", cfg.Storage.Kind, err)
	}
	a.store = st

	// the engine checks ffmpeg and vips
	e, err := engine.New(cfg, st, a.log)
	if err != nil {
		return err
	}
	a.engine = e

	// nats
	if cfg.NATS.Enabled() {
		cons, err := natsserver.Open(ctx, cfg.NATS, cfg.WorkerID, a.log)
		if err != nil {
			return err
		}
		a.nats = cons
		a.probes = append(a.probes, cons)
	}

	a.log.Info("bee: started", a.describe()...)
	return nil
}

// describe is what the start log tells: version, storage and transports,
// never a secret.
func (a *app) describe() []any {
	cfg := a.cfg

	version := os.Getenv("BEE_VERSION")
	if version == "" {
		version = "dev"
	}

	// the storage without keys
	where := cfg.Storage.Local.Dir
	if cfg.Storage.Kind == "s3" {
		where = cfg.Storage.S3.Provider + ":" + cfg.Storage.S3.Bucket
		if cfg.Storage.S3.Prefix != "" {
			where += "/" + cfg.Storage.S3.Prefix
		}
	}

	// nats by its job subjects, when on
	nats := ""
	if cfg.NATS.Enabled() {
		nats = cfg.NATS.Names().JobsSubject()
	}

	return []any{
		"version", version,
		"worker_id", cfg.WorkerID,
		"storage", cfg.Storage.Kind,
		"storage_at", where,
		"health", cfg.Health.Addr,
		"http", a.off(cfg.HTTP.Addr),
		"grpc", a.off(cfg.GRPC.Addr),
		"nats", a.off(nats),
		"concurrency", cfg.Concurrency,
	}
}

// off is value, or "off" when it is empty.
func (a *app) off(value string) string {
	if value == "" {
		return "off"
	}

	return value
}

// serve starts the health server always and every transport that is on.
func (a *app) serve(ctx context.Context) {
	cfg := a.cfg

	// health
	probe := &health.Server{Engine: a.engine, Probes: a.probes}
	a.listen(ctx, "health", cfg.Health.Addr, probe.Serve)

	// http
	if cfg.HTTP.Enabled() {
		srv := &httpserver.Server{Engine: a.engine, Token: cfg.Token, Timeout: a.longest()}
		a.listen(ctx, "http", cfg.HTTP.Addr, srv.Serve)
	}

	// grpc
	if cfg.GRPC.Enabled() {
		srv := &grpcserver.Server{Engine: a.engine, Token: cfg.Token}
		a.listen(ctx, "grpc", cfg.GRPC.Addr, srv.Serve)
	}

	// nats
	if a.nats != nil {
		a.wg.Add(1)
		go a.pull(ctx)
	}
}

// listen runs one server on addr in its own goroutine.
func (a *app) listen(ctx context.Context, name, addr string, serve func(context.Context, net.Listener, time.Duration) error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		a.errc <- fmt.Errorf("%s: listen %s: %w", name, addr, err)
		return
	}

	a.wg.Add(1)
	go a.server(ctx, name, lis, serve)
}

// server runs serve until ctx is done; an error before that is a failure.
func (a *app) server(ctx context.Context, name string, lis net.Listener, serve func(context.Context, net.Listener, time.Duration) error) {
	defer a.wg.Done()

	err := serve(ctx, lis, a.cfg.ShutdownTimeout)
	switch {
	case err != nil:
		a.errc <- fmt.Errorf("%s: %w", name, err)
	case ctx.Err() == nil:
		a.errc <- errors.New(name + ": stopped")
	}
}

// pull runs the nats consumer until ctx is done.
func (a *app) pull(ctx context.Context) {
	defer a.wg.Done()

	a.nats.Run(ctx, a.engine, a.cfg.Concurrency, a.cfg.ShutdownTimeout)
}

// longest is the longest job timeout of any type.
func (a *app) longest() time.Duration {
	cfg := a.cfg

	return max(cfg.Audio.Timeout, cfg.Video.Timeout, cfg.Images.Timeout)
}

// health asks /health of the local health server; the exit code.
func (a *app) health() int {
	addr := os.Getenv("BEE_HEALTH_ADDR")
	if addr == "" {
		addr = config.Defaults().Health.Addr
	}

	// ":8081" is this host
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, "bee health:", err)
		return 1
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "bee health: status", resp.StatusCode)
		return 1
	}

	return 0
}

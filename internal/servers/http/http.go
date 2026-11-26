// Package http accepts jobs over HTTP/JSON: POST /v1/audio, /v1/stream,
// /v1/video and /v1/images take the json of a job and answer the json of
// its result, with the HTTP status of its error code. Every request needs
// "Authorization: Bearer <BEE_TOKEN>". Health, readiness and metrics live
// on the health server, not here.
package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/angels-you/bee/internal/engine"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/code"
)

// Server serves the jobs of one engine. Token is required.
type Server struct {
	Engine  *engine.Engine
	Token   string
	Timeout time.Duration // the longest job: writing the answer waits for it

	names   proto.Names
	maxBody int64 // a job is a few kilobytes of json
}

// Handler is the router of the job endpoints.
func (a *Server) Handler() http.Handler {
	a.maxBody = 1 << 20

	mux := http.NewServeMux()
	for _, kind := range proto.Kinds() {
		mux.Handle("POST "+a.names.Path(kind), a.job(kind))
	}

	return mux
}

// job is the handler of the jobs of kind.
func (a *Server) job(kind proto.Kind) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.process(w, r, kind)
	})
}

// process takes the json of a job and answers the json of its result
// with the status of its error code.
func (a *Server) process(w http.ResponseWriter, r *http.Request, kind proto.Kind) {

	// the token
	if !a.authorized(r) {
		e := code.New(code.InvalidJob, "bad or missing token", nil)
		a.reply(w, a.failure(e), http.StatusUnauthorized)
		return
	}

	// the body
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, a.maxBody))
	if err != nil {
		e := code.New(code.InvalidJob, "body: "+err.Error(), nil)
		a.reply(w, a.failure(e), http.StatusBadRequest)
		return
	}

	// the job
	res, err := a.Engine.Do(r.Context(), kind, data)
	a.reply(w, res, a.status(err))
}

// authorized checks "Authorization: Bearer" in constant time; an empty
// Token (refused by the config) lets nothing through.
func (a *Server) authorized(r *http.Request) bool {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))

	switch {
	case a.Token == "":
		return false
	case token == "":
		return false
	}

	return subtle.ConstantTimeCompare([]byte(token), []byte(a.Token)) == 1
}

// failure is the json of a result without a job: the error only.
func (a *Server) failure(e *code.Error) []byte {
	data, _ := json.Marshal(struct {
		Error *code.Error `json:"error"`
	}{e})

	return data
}

// status is the HTTP status of the error of a job; 200 without one.
func (a *Server) status(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch code.As(err).Code {
	case code.InvalidJob, code.InvalidPath, code.InvalidDimensions, code.InvalidQuality:
		return http.StatusBadRequest
	case code.UnsupportedCodec, code.UnsupportedFormat, code.InvalidAudio, code.InvalidVideo, code.InvalidImage,
		code.InvalidSound, code.InvalidWatermark, code.DimensionLimitExceeded, code.PixelLimitExceeded,
		code.DurationLimitExceeded, code.NoRendition, code.OutputTooLarge, code.EncodeFailed, code.VerifyFailed:
		return http.StatusUnprocessableEntity
	case code.SourceTooLarge:
		return http.StatusRequestEntityTooLarge
	case code.SourceUrlExpired:
		return http.StatusGone
	case code.SourceUnavailable:
		return http.StatusFailedDependency
	case code.DownloadTimeout, code.StorageTimeout:
		return http.StatusGatewayTimeout
	case code.StorageUnavailable:
		return http.StatusBadGateway
	case code.InternalTemporaryError:
		return http.StatusServiceUnavailable
	}

	return http.StatusInternalServerError
}

// reply writes json with status.
func (a *Server) reply(w http.ResponseWriter, body []byte, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

// Serve listens on lis until ctx is done, then stops: running requests
// get shutdown to finish, the rest are cut.
func (a *Server) Serve(ctx context.Context, lis net.Listener, shutdown time.Duration) error {
	srv := &http.Server{
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      a.Timeout + 30*time.Second,
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

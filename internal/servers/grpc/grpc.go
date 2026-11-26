// Package grpc accepts jobs over gRPC: the Bee service of pkg/proto
// (Audio, Stream, Video, Images; the json of a job in, the json of its
// result out). A failed job is a status with the bee code in the message
// and in the trailers bee-error-code and bee-retryable. Every call needs
// "authorization: Bearer <BEE_TOKEN>" metadata
package grpc

import (
	"context"
	"crypto/subtle"
	"net"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/angels-you/bee/internal/engine"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/code"
)

// Server is proto.BeeServer over one engine. Token is required
type Server struct {
	proto.UnimplementedBeeServer

	Engine *engine.Engine
	Token  string

	names proto.Names
}

// NewGRPCServer is a grpc.Server with the token check and the Bee service
func (a *Server) NewGRPCServer(opts ...grpc.ServerOption) *grpc.Server {
	opts = append(opts, grpc.UnaryInterceptor(a.auth))

	g := grpc.NewServer(opts...)
	proto.RegisterBeeServer(g, a)
	return g
}

// Serve listens on lis until ctx is done, then stops: running calls get
// shutdown to finish, the rest are cut
func (a *Server) Serve(ctx context.Context, lis net.Listener, shutdown time.Duration) error {
	g := a.NewGRPCServer()

	// the server
	errc := make(chan error, 1)
	go a.listen(g, lis, errc)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// the stop: graceful, then hard
	stopped := make(chan struct{})
	go a.stop(g, stopped)

	t := time.NewTimer(shutdown)
	defer t.Stop()

	select {
	case <-stopped:
	case <-t.C:
		g.Stop()
	}

	return nil
}

// listen runs Serve into errc
func (a *Server) listen(g *grpc.Server, lis net.Listener, errc chan<- error) {
	errc <- g.Serve(lis)
}

// stop runs GracefulStop and signals its end
func (a *Server) stop(g *grpc.Server, stopped chan<- struct{}) {
	g.GracefulStop()
	close(stopped)
}

// auth checks the bearer token in constant time; an empty Token
// (refused by the config) lets nothing through
func (a *Server) auth(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if a.Token == "" {
		return nil, status.Error(codes.Unauthenticated, "no token is configured")
	}

	md, _ := metadata.FromIncomingContext(ctx)
	for _, v := range md.Get("authorization") {
		token := strings.TrimSpace(strings.TrimPrefix(v, "Bearer"))
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.Token)) == 1 {
			return handler(ctx, req)
		}
	}

	return nil, status.Error(codes.Unauthenticated, "bad or missing token")
}

// Audio runs an audio job
func (a *Server) Audio(ctx context.Context, in *proto.Job) (*proto.Result, error) {
	return a.do(ctx, proto.Audio, in)
}

// Stream runs an HLS job
func (a *Server) Stream(ctx context.Context, in *proto.Job) (*proto.Result, error) {
	return a.do(ctx, proto.Stream, in)
}

// Video runs an MP4 job
func (a *Server) Video(ctx context.Context, in *proto.Job) (*proto.Result, error) {
	return a.do(ctx, proto.Video, in)
}

// Images runs an images job
func (a *Server) Images(ctx context.Context, in *proto.Job) (*proto.Result, error) {
	return a.do(ctx, proto.Images, in)
}

// do answers the result of a done job or the status of a failed one with
// its code in the trailers
func (a *Server) do(ctx context.Context, kind proto.Kind, in *proto.Job) (*proto.Result, error) {
	res, err := a.Engine.Do(ctx, kind, in.GetJson())
	if err == nil {
		return &proto.Result{Json: res}, nil
	}

	e := code.As(err)
	trailer := metadata.Pairs(a.names.TrailerCode(), string(e.Code), a.names.TrailerRetryable(), strconv.FormatBool(e.Retryable))
	grpc.SetTrailer(ctx, trailer)

	return nil, status.Error(a.status(e.Code), string(e.Code)+": "+e.Message)
}

// status is the gRPC code of a bee code
func (a *Server) status(c code.Code) codes.Code {
	switch c {
	case code.InvalidJob, code.InvalidPath, code.InvalidDimensions, code.InvalidQuality, code.UnsupportedCodec,
		code.UnsupportedFormat, code.InvalidAudio, code.InvalidVideo, code.InvalidImage, code.InvalidSound,
		code.InvalidWatermark, code.SourceTooLarge, code.DimensionLimitExceeded, code.PixelLimitExceeded,
		code.DurationLimitExceeded, code.NoRendition, code.OutputTooLarge:
		return codes.InvalidArgument
	case code.SourceUrlExpired:
		return codes.FailedPrecondition
	case code.DownloadTimeout, code.StorageTimeout:
		return codes.DeadlineExceeded
	case code.SourceUnavailable, code.StorageUnavailable, code.InternalTemporaryError:
		return codes.Unavailable
	}

	return codes.Internal
}

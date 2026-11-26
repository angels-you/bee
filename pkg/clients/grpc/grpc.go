// Package beegrpc is the Go client of bee over gRPC. One connection is
// reused by every call; one call is one job.
//
//	c, err := beegrpc.Dial("bee:9090", false, os.Getenv("BEE_TOKEN"))
//	if err != nil {
//		return err
//	}
//	defer c.Close()
//
//	res, err := c.GenerateAudio(ctx, audio.Job{
//		ID:     "track-1",
//		Input:  presignedURL,
//		PlaylistPath: "tracks/1/master.m3u8",
//		WavePath:   "tracks/1/wave.json",
//	})
//
// A failed job returns its result with Error set and the same *code.Error
// as the error, rebuilt from the status and its trailers.
package beegrpc

import (
	"context"
	"encoding/json"
	"github.com/angels-you/bee/pkg/tools"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
	"github.com/angels-you/bee/pkg/types/video"
)

// Client calls one bee over gRPC.
type Client struct {
	conn  *grpc.ClientConn
	api   proto.BeeClient
	token string
	names proto.Names
}

// Dial connects to addr "host:port"; tls false for a bee in the same
// network, true behind a TLS proxy. token is BEE_TOKEN.
func Dial(addr string, tls bool, token string, opts ...grpc.DialOption) (*Client, error) {
	creds := insecure.NewCredentials()
	if tls {
		creds = credentials.NewTLS(nil)
	}

	opts = append(opts, grpc.WithTransportCredentials(creds))
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, err
	}

	return &Client{conn: conn, api: proto.NewBeeClient(conn), token: token}, nil
}

// Close closes the connection.
func (a *Client) Close() error {
	return a.conn.Close()
}

// GenerateAudio runs an audio job.
func (a *Client) GenerateAudio(ctx context.Context, j audio.Job) (audio.Result, error) {
	var res audio.Result
	err := a.call(ctx, proto.Audio, j.ID, j.Meta, j, &res)

	return res, err
}

// GenerateStream runs an HLS job.
func (a *Client) GenerateStream(ctx context.Context, j video.StreamJob) (video.Result, error) {
	var res video.Result
	err := a.call(ctx, proto.Stream, j.ID, j.Meta, j, &res)

	return res, err
}

// GenerateVideo runs an MP4 job.
func (a *Client) GenerateVideo(ctx context.Context, j video.VideoJob) (video.Result, error) {
	var res video.Result
	err := a.call(ctx, proto.Video, j.ID, j.Meta, j, &res)

	return res, err
}

// GenerateImages runs an images job.
func (a *Client) GenerateImages(ctx context.Context, j images.Job) (images.Result, error) {
	var res images.Result
	err := a.call(ctx, proto.Images, j.ID, j.Meta, j, &res)

	return res, err
}

// call sends job to the method of kind and reads the result into res.
func (a *Client) call(ctx context.Context, kind proto.Kind, id any, meta map[string]any, job, res any) error {
	data, err := json.Marshal(job)
	if err != nil {
		return a.fail(res, id, meta, code.New(code.InvalidJob, "job json", err))
	}

	// the token
	if a.token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.token)
	}

	// the call
	var trailer metadata.MD
	out, err := a.invoke(ctx, kind, &proto.Job{Json: data}, grpc.Trailer(&trailer))
	if err != nil {
		return a.fail(res, id, meta, a.error(err, trailer))
	}

	// the result
	err = tools.Decode(out.GetJson(), res)
	if err != nil {
		return a.fail(res, id, meta, code.New(code.InternalTemporaryError, "bee grpc: unreadable result", err))
	}

	return nil
}

// invoke calls the method of kind.
func (a *Client) invoke(ctx context.Context, kind proto.Kind, in *proto.Job, opts ...grpc.CallOption) (*proto.Result, error) {
	switch kind {
	case proto.Audio:
		return a.api.Audio(ctx, in, opts...)
	case proto.Stream:
		return a.api.Stream(ctx, in, opts...)
	case proto.Video:
		return a.api.Video(ctx, in, opts...)
	}

	return a.api.Images(ctx, in, opts...)
}

// error rebuilds the bee error of a failed call from the trailers and the
// "<CODE>: <message>" of the status; a call that never reached bee (no
// trailer) is INTERNAL_TEMPORARY_ERROR.
func (a *Client) error(err error, trailer metadata.MD) *code.Error {
	st := status.Convert(err)

	// no bee code: the transport failed
	values := trailer.Get(a.names.TrailerCode())
	if len(values) == 0 {
		if st.Code() == codes.Unauthenticated {
			return code.New(code.InvalidJob, "bee grpc: "+st.Message(), err)
		}
		return code.New(code.InternalTemporaryError, "bee grpc: "+st.Message(), err)
	}

	// the bee code and its message
	c := code.Code(values[0])
	message := strings.TrimPrefix(st.Message(), string(c)+": ")

	return code.New(c, message, nil)
}

// fail fills res as a failure and returns e.
func (a *Client) fail(res any, id any, meta map[string]any, e *code.Error) error {
	proto.Failure(res, id, meta, e)
	return e
}

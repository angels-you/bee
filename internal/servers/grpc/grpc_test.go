package grpc_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/angels-you/bee/internal/servers/grpc"
	"github.com/angels-you/bee/internal/testmedia/testbee"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/images"
)

// TestBee — the token, a done job and a failed one with its trailers.
func TestBee(t *testing.T) {
	bench := &testbee.Bench{T: t}
	bench.Start()

	// the server
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &grpc.Server{Engine: bench.Engine, Token: "test-token"}
	go srv.Serve(ctx, lis, time.Second)

	// the client
	conn, err := ggrpc.NewClient(lis.Addr().String(), ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	api := proto.NewBeeClient(conn)

	job, _ := json.Marshal(bench.Images("g1", "grpc/1"))

	// no token
	_, err = api.Images(ctx, &proto.Job{Json: job})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("no token: %v", err)
	}

	// a done job
	authed := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer test-token")
	out, err := api.Images(authed, &proto.Job{Json: job})
	if err != nil {
		t.Fatal(err)
	}

	var res images.Result
	err = json.Unmarshal(out.Json, &res)
	if err != nil || len(res.Tasks) != 2 {
		t.Errorf("result %s %v", out.Json, err)
	}

	// a failed job
	var trailer metadata.MD
	_, err = api.Audio(authed, &proto.Job{Json: []byte(`{"id":"x"}`)}, ggrpc.Trailer(&trailer))
	switch {
	case status.Code(err) != codes.InvalidArgument:
		t.Errorf("bad job: %v", err)
	case len(trailer.Get("bee-error-code")) == 0:
		t.Error("no error code trailer")
	case trailer.Get("bee-error-code")[0] != "INVALID_JOB":
		t.Errorf("code trailer %v", trailer.Get("bee-error-code"))
	}
}

// Command grpc sends one audio job to bee over gRPC and prints the result.
//
//	BEE_ADDR=localhost:9090 BEE_TOKEN=change-me \
//	go run ./examples/grpc -input https://example.com/track.mp3
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	beegrpc "github.com/angels-you/bee/pkg/clients/grpc"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
)

func main() {
	addr := flag.String("addr", env("BEE_ADDR", "localhost:9090"), "bee grpc host:port")
	tls := flag.Bool("tls", false, "true behind a TLS proxy")
	token := flag.String("token", os.Getenv("BEE_TOKEN"), "BEE_TOKEN of the bee")
	input := flag.String("input", os.Getenv("BEE_INPUT"), "https url of the original track")
	flag.Parse()

	c, err := beegrpc.Dial(*addr, *tls, *token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer c.Close()

	// HLS renditions, a waveform and the metadata; the keys must be new
	job := audio.Job{
		ID:      "track-1",
		Input:   *input,
		Output:  "tracks/1/master.m3u8",
		Wave:    "tracks/1/wave.json",
		Meta:    "tracks/1/meta.json",
		Options: audio.Options{Normalize: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	res, err := c.Audio(ctx, job)
	if err != nil {
		e := code.As(err)
		fmt.Fprintf(os.Stderr, "failed: %s %s (retryable %v)\n", e.Code, e.Message, e.Retryable)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
}

// env returns the env key or def when it is empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

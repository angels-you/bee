// Command nats sends one HLS video job to bee over NATS JetStream and
// prints the result.
//
//	NATS_URL=nats://localhost:4222 \
//	go run ./examples/nats -input https://example.com/clip.mp4
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/angels-you/bee/pkg/clients/nats"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/video"
)

func main() {
	url := flag.String("url", env("NATS_URL", "nats://localhost:4222"), "nats url")
	token := flag.String("token", os.Getenv("NATS_TOKEN"), "nats auth token, empty: none")
	input := flag.String("input", os.Getenv("BEE_INPUT"), "https url of the original video")
	flag.Parse()

	// proto.Names{} are the default subjects bee.jobs.new and bee.jobs.done
	c, err := beenats.Dial(*url, *token, proto.Names{Jobs: os.Getenv("BEE_NATS_JOBS"), Done: os.Getenv("BEE_NATS_DONE")})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer c.Close()

	// vertical 9:16 HLS with two renditions and a cover
	job := video.Stream{
		ID:         "clip-1",
		Input:      *input,
		Output:     "clips/1/index.m3u8",
		Options:    video.VerticalOptions(),
		Profiles:   video.VerticalMobile(),
		HLS:        video.DefaultHLS(),
		Thumbnails: []video.Thumbnail{{Output: "clips/1/cover.webp", Format: video.ImageWebP, Width: 720}},
	}

	// the wait ends with ctx; the job still runs and c.Result reads it later
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	defer cancel()

	res, err := c.Stream(ctx, job)
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

// Command nats sends one HLS video job to bee over NATS JetStream, or runs
// a worker that prints every result.
//
//	# terminal 1: the worker of your service
//	go run ./examples/nats -work
//
//	# terminal 2: send a job; it returns once NATS has it
//	go run ./examples/nats -input https://example.com/clip.mp4
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/angels-you/bee/pkg/clients/nats"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/video"
)

func main() {
	url := flag.String("url", "nats://localhost:4222", "nats url")
	token := flag.String("token", os.Getenv("NATS_TOKEN"), "nats auth token, empty: none")
	input := flag.String("input", "", "https url of the original video")
	work := flag.Bool("work", false, "run the worker that prints the results")
	flag.Parse()

	// proto.Names{} are the default subjects bee.jobs and bee.done
	c, err := beenats.Dial(*url, *token, proto.Names{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer c.Close()

	// stops on ctrl+c
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// the worker
	if *work {
		err = c.Done(ctx, "example", show)
		if err != nil {
			fmt.Fprintln(os.Stderr, "done:", err)
			os.Exit(1)
		}
		return
	}

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

	// send and forget: the result comes to the worker
	err = c.Stream(ctx, job)
	if err != nil {
		fmt.Fprintln(os.Stderr, "send:", err)
		os.Exit(1)
	}
	fmt.Println("sent", job.ID)
}

// show prints one result; returning nil acks it.
func show(m proto.Envelope) error {
	// a failed job
	e := m.Failure()
	if e != nil {
		fmt.Printf("%s %s failed: %s %s (retryable %v)\n", m.Kind, m.ID, e.Code, e.Message, e.Retryable)
		return nil
	}

	// a done job
	out, _ := json.MarshalIndent(m.Result, "", "  ")
	fmt.Printf("%s %s done:\n%s\n", m.Kind, m.ID, out)
	return nil
}

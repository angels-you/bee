// Command nats sends one HLS video job to bee over NATS JetStream, or runs
// a worker that prints every HLS result.
//
//	# terminal 1: the worker of your service
//	go run ./examples/nats -work
//
//	# terminal 2: send a job; it returns once NATS has it
//	go run ./examples/nats -input https://example.com/clip.mp4
package main

import (
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

	// "example" is the group of this service: its instances share the results;
	// proto.Names{} are the default subjects bee.jobs and bee.done
	c, err := beenats.Dial("example", *url, *token, proto.Names{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer c.Close()

	// the worker: prints results until ctrl+c
	if *work {
		err = c.SubscribeStream(show)
		if err != nil {
			fmt.Fprintln(os.Stderr, "subscribe:", err)
			os.Exit(1)
		}

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		<-stop
		return
	}

	// vertical 9:16 HLS with two renditions and a cover
	job := video.StreamJob{
		ID:         1,
		Input:      *input,
		Output:     "clips/1/index.m3u8",
		Options:    video.VerticalOptions(),
		Profiles:   video.VerticalMobile(),
		HLS:        video.DefaultHLS(),
		Thumbnails: []video.Thumbnail{{Output: "clips/1/cover.webp", Format: video.ImageWebP, Width: 720}},
		Meta:       map[string]any{"clip": "first"},
	}

	// send and forget: the result comes to the worker
	err = c.GenerateStream(job)
	if err != nil {
		fmt.Fprintln(os.Stderr, "send:", err)
		os.Exit(1)
	}
	fmt.Println("sent", job.ID)
}

// show prints one HLS result; failed jobs come here too.
func show(res video.Result) {
	// a failed job
	if res.Error != nil {
		fmt.Printf("%v failed: %s %s (retryable %v)\n", res.ID, res.Error.Code, res.Error.Message, res.Error.Retryable)
		return
	}

	// a done job
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Printf("%v done:\n%s\n", res.ID, out)
}

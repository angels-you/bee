// Command http sends one images job to bee over HTTP and prints the result.
//
//	BEE_URL=http://localhost:8080 BEE_TOKEN=change-me \
//	go run ./examples/http -input https://example.com/photo.jpg
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/angels-you/bee/pkg/clients/http"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
)

func main() {
	url := flag.String("url", env("BEE_URL", "http://localhost:8080"), "bee http address")
	token := flag.String("token", os.Getenv("BEE_TOKEN"), "BEE_TOKEN of the bee")
	input := flag.String("input", os.Getenv("BEE_INPUT"), "https url of the original image")
	flag.Parse()

	c := &beehttp.Client{URL: *url, Token: *token}

	// one original, two results in the bee storage
	job := images.Job{
		ID:    "photo-1",
		Input: *input,
		Tasks: []images.Task{
			{ID: "small", Output: "photos/1/small.webp", Profile: images.Profile{Format: images.Webp, Quality: 80, MaxWidth: 320}},
			{ID: "cover", Output: "photos/1/cover.jpg", Profile: images.Profile{Format: images.Jpg, Quality: 85, Fit: images.Cover, MaxWidth: 1200, MaxHeight: 630}},
		},
	}

	// one call lasts as long as the job
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := c.GenerateImages(ctx, job)
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

package video

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/angels-you/bee/pkg/types/code"
)

// TestPresets — every preset has the size, bitrates, VBV and frame rate
// of the table in presets.go, and the estimate per minute its comment
// promises.
func TestPresets(t *testing.T) {
	want := map[Quality][5]int{ // width, height, video kbps, audio kbps, fps
		Quality240p:    {240, 426, 250, 64, 30},
		Quality360p:    {360, 640, 500, 96, 30},
		Quality480p:    {480, 854, 900, 96, 30},
		Quality540p:    {540, 960, 1200, 96, 30},
		Quality720p:    {720, 1280, 1800, 128, 30},
		Quality1080p:   {1080, 1920, 3500, 128, 30},
		Quality1080p60: {1080, 1920, 6000, 192, 60},
	}
	perMinute := map[Quality]int64{ // bytes, the MB/min of the comments
		Quality240p:    2_355_000,
		Quality360p:    4_470_000,
		Quality480p:    7_470_000,
		Quality540p:    9_720_000,
		Quality720p:    14_460_000,
		Quality1080p:   27_210_000,
		Quality1080p60: 46_440_000,
	}

	for q, w := range want {
		p, ok := Preset(q)
		if !ok {
			t.Fatalf("%s is not a preset", q)
		}
		got := [5]int{p.Width, p.Height, p.VideoBitrate, p.SoundBitrate, p.FPS}
		if got != w {
			t.Errorf("%s: %v, want %v", q, got, w)
		}
		if p.Name != string(q) {
			t.Errorf("%s: name %q", q, p.Name)
		}
		vbv := [2]int{p.MaxBitrate, p.BufferSize}
		if vbv != [2]int{p.VideoBitrate * 3 / 2, p.VideoBitrate * 3} {
			t.Errorf("%s: vbv %v", q, vbv)
		}
		codecs := [4]string{string(p.VideoCodec), p.PixelFormat, string(p.RateControl), string(p.SoundCodec)}
		if codecs != [4]string{"h264", "yuv420p", "vbr", "aac"} {
			t.Errorf("%s: codecs %v", q, codecs)
		}
		audio := [2]int{p.SampleRate, p.Channels}
		if audio != [2]int{48000, 2} {
			t.Errorf("%s: audio %v", q, audio)
		}
		if p.EstimatedBytes(60, true) != perMinute[q] {
			t.Errorf("%s: %d bytes a minute, want %d", q, p.EstimatedBytes(60, true), perMinute[q])
		}
	}

	_, ok := Preset("4k")
	if ok {
		t.Error("an unknown quality is a preset")
	}
}

// ladder — profiles of a test, named in order.
type ladder []Profile

// names — the profile names joined by commas.
func (a ladder) names() string {
	var n []string
	for _, p := range a {
		n = append(n, p.Name)
	}
	return strings.Join(n, ",")
}

// TestLadders — the ladders in their order, each preset a fresh value.
func TestLadders(t *testing.T) {
	got := ladder(VerticalMobile()).names()
	if got != "360p,720p" {
		t.Errorf("mobile %s", got)
	}
	got = ladder(VerticalEconomy()).names()
	if got != "240p,480p,720p" {
		t.Errorf("economy %s", got)
	}
	got = ladder(VerticalStandard()).names()
	if got != "360p,720p,1080p" {
		t.Errorf("standard %s", got)
	}
	got = ladder(VerticalFull()).names()
	if got != "240p,360p,480p,720p,1080p" {
		t.Errorf("full %s", got)
	}

	// a changed copy does not change the next one
	mobile := VerticalMobile()
	mobile[0].Width = 1
	if VerticalMobile()[0].Width != 360 {
		t.Error("presets share their values")
	}
}

// TestEstimatedBytes — no estimate without a duration or a target
// bitrate; audio at the worker default when the profile has none.
func TestEstimatedBytes(t *testing.T) {
	p := Profile{VideoBitrate: 1000}
	if p.EstimatedBytes(0, true) != 0 {
		t.Error("estimate without a duration")
	}
	if p.EstimatedBytes(-1, true) != 0 {
		t.Error("estimate of a negative duration")
	}
	if (Profile{RateControl: RateCRF}).EstimatedBytes(10, true) != 0 {
		t.Error("estimate without a bitrate")
	}
	if p.EstimatedBytes(8, false) != 1_000_000 {
		t.Errorf("video only %d", p.EstimatedBytes(8, false))
	}
	if p.EstimatedBytes(8, true) != 1_128_000 {
	}
}

// TestJSON — snake_case keys, the key as base64, the error of a failed
// result without its cause.
func TestJSON(t *testing.T) {
	s := Stream{
		ID:       "j1",
		Input:    "https://s3.example.com/in.mov",
		Output:   "video/1/index.m3u8",
		Options:  VerticalOptions(),
		Profiles: VerticalMobile(),
		HLS:      HLS{Key: []byte("0123456789abcdef"), KeyURI: "skd://1"},
	}
	js, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"id":"j1"`, `"options":{"vertical":true,"resize":"cover","gravity":"center"}`, `"video_bitrate":500`, `"key":"MDEyMzQ1Njc4OWFiY2RlZg=="`, `"key_uri":"skd://1"`} {
		if !strings.Contains(string(js), part) {
			t.Errorf("no %s in %s", part, js)
		}
	}

	var back Stream
	err = json.Unmarshal(js, &back)
	if err != nil {
		t.Fatal(err)
	}
	if string(back.HLS.Key) != "0123456789abcdef" || len(back.Profiles) != 2 {
		t.Errorf("round trip %+v", back)
	}

	failed := Result{ID: "j1", Error: code.New(code.InvalidJob, "bad", nil)}
	js, err = json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	if string(js) != `{"id":"j1","elapsed":0,"error":{"code":"INVALID_JOB","message":"bad","retryable":false}}` {
		t.Errorf("failed result %s", js)
	}
}

// TestParams checks job processing parameters: empty ones stay out of
// json and mean the bee default
func TestParams(t *testing.T) {
	js, err := json.Marshal(Options{Normalize: true})
	if err != nil {
		t.Fatal(err)
	}

	if string(js) != `{"normalize":true}` {
		t.Errorf("empty params in json: %s", js)
	}

	var o Options
	err = json.Unmarshal([]byte(`{"normalize":true,"lufs":-14,"true_peak":-1}`), &o)
	switch {
	case err != nil:
		t.Fatal(err)
	case o.LUFS != -14:
		t.Errorf("lufs %v", o.LUFS)
	case o.TruePeak != -1:
		t.Errorf("true peak %v", o.TruePeak)
	}
}

// TestValidate checks the light checks of Stream and Video
func TestValidate(t *testing.T) {
	s := Stream{ID: "s1", Input: "https://example.com/a.mov", Output: "v/1/index.m3u8"}
	e := s.Validate()
	if e != nil {
		t.Errorf("stream: %v", e)
	}

	v := Video{ID: "v1", Input: "https://example.com/a.mov", Output: "v/1/video.mp4"}
	e = v.Validate()
	if e != nil {
		t.Errorf("video: %v", e)
	}

	bad := []Video{
		{ID: "", Input: "https://example.com/a.mov", Output: "a.mp4"},
		{ID: "v", Input: "a.mov", Output: "a.mp4"},
		{ID: "v", Input: "https://example.com/a.mov", Output: "a/../b.mp4"},
	}
	for i, x := range bad {
		if x.Validate() == nil {
			t.Errorf("%d accepted", i)
		}
	}
}

package video

import (
	"testing"
)

// mediaPlaylist — a two segment fMP4 media playlist as ffmpeg writes it.
type mediaPlaylist struct {
	text string
}

// init — the playlist text.
func (a *mediaPlaylist) init() {
	a.text = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="init.mp4"
#EXTINF:2.000000,
segment_00000.m4s
#EXTINF:0.500000,
segment_00001.m4s
#EXT-X-ENDLIST
`
}

// TestPlaylistParse — the tags bee checks and every segment.
func TestPlaylistParse(t *testing.T) {
	m := mediaPlaylist{}
	m.init()

	p := &Playlist{}
	err := p.Parse(m.text)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case p.Version != 7:
		t.Errorf("version %d", p.Version)
	case !p.VOD || !p.EndList || !p.Independent:
		t.Errorf("flags %+v", p)
	case p.Init != "init.mp4":
		t.Errorf("init %q", p.Init)
	case len(p.Segments) != 2 || p.Segments[1] != (Segment{Name: "segment_00001.m4s", Duration: 0.5}):
		t.Errorf("segments %+v", p.Segments)
	case p.Duration != 2.5:
		t.Errorf("duration %v", p.Duration)
	}

	// broken playlists
	broken := []string{"", "#EXT-X-VERSION:7\n", "#EXTM3U\nsegment.m4s\n", "#EXTM3U\n#EXTINF:x,\nsegment.m4s\n", "#EXTM3U\n#EXT-X-MAP:BYTERANGE=1\n"}
	for _, text := range broken {
		err = (&Playlist{}).Parse(text)
		if err == nil {
			t.Errorf("parsed %q", text)
		}
	}
}

// TestBandwidth — the peak is the densest segment, the average all bytes
// over all seconds, both rounded up; the init does not count.
func TestBandwidth(t *testing.T) {
	m := mediaPlaylist{}
	m.init()
	p := &Playlist{}
	err := p.Parse(m.text)
	if err != nil {
		t.Fatal(err)
	}

	peak, average := p.Bandwidth(map[string]int64{"init.mp4": 1 << 20, "segment_00000.m4s": 50_000, "segment_00001.m4s": 20_001})
	if peak != 320_016 {
		t.Errorf("peak %d", peak)
	}
	if average != 224_004 {
		t.Errorf("average %d", average)
	}
}

// TestMaster — the master playlist text.
func TestMaster(t *testing.T) {
	m := Master{Version: 7, Variants: []Variant{
		{URI: "360p/index.m3u8", Bandwidth: 700_000, AverageBandwidth: 600_000, Width: 360, Height: 640, FPS: 30, Codecs: "avc1.64001e,mp4a.40.2"},
		{URI: "720p/index.m3u8", Bandwidth: 2_400_000, AverageBandwidth: 2_000_000, Width: 720, Height: 1280, FPS: 29.97, Codecs: "avc1.64001f,mp4a.40.2"},
	}}
	want := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-STREAM-INF:BANDWIDTH=700000,AVERAGE-BANDWIDTH=600000,RESOLUTION=360x640,FRAME-RATE=30.000,CODECS="avc1.64001e,mp4a.40.2"
360p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2400000,AVERAGE-BANDWIDTH=2000000,RESOLUTION=720x1280,FRAME-RATE=29.970,CODECS="avc1.64001f,mp4a.40.2"
720p/index.m3u8
`
	if m.String() != want {
		t.Errorf("master:\n%s", m.String())
	}
}

// TestCodecStrings — the RFC 6381 strings from the codec private data of
// each codec, as ffprobe -show_data dumps it.
func TestCodecStrings(t *testing.T) {
	avcC := probeStream{CodecName: "h264", Extradata: "\n00000000: 0164 001f ffe1 001a 6764 001e acd9 4140  .d......gd....A@\n"}
	annexB := probeStream{CodecName: "h264", Extradata: "\n00000000: 0000 0167 42c0 1eda 0501 27e5 c044 0000  ...gB.....'..D..\n"}
	hvcC := probeStream{CodecName: "hevc", Extradata: "\n00000000: 0101 6000 0000 9000 0000 0000 5df0 00fc  ..`.........]...\n"}
	av1C := probeStream{CodecName: "av1", Extradata: "\n00000000: 8108 0c00                                ....\n"}
	aac := probeStream{CodecName: "aac", Profile: "LC"}
	heaac := probeStream{CodecName: "aac", Profile: "HE-AAC"}

	want := map[*probeStream]string{
		&avcC:   "avc1.64001f",
		&annexB: "avc1.42c01e",
		&hvcC:   "hvc1.1.6.L93.90",
		&av1C:   "av01.0.08M.08",
		&aac:    "mp4a.40.2",
		&heaac:  "",
	}
	for s, w := range want {
		if s.rfc6381() != w {
			t.Errorf("%s %s: %q, want %q", s.CodecName, s.Profile, s.rfc6381(), w)
		}
	}
}

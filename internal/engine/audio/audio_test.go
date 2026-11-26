package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angels-you/bee/internal/testmedia/testaudio"
)

// bins — ffmpeg and ffprobe from PATH
var bins = Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return c
}

func TestProbe(t *testing.T) {
	testaudio.Skip(t)
	dir := t.TempDir()
	tools := bins
	cases := []struct {
		opts  testaudio.Opts
		codec string
	}{
		{testaudio.Opts{Name: "a.mp3", Title: "It's all right", Artist: "TFNY"}, "mp3"},
		{testaudio.Opts{Name: "a.flac", Channels: 1}, "flac"},
		{testaudio.Opts{Name: "a.wav", Rate: 48000}, "pcm_s16le"},
	}
	for _, c := range cases {
		file := testaudio.Generate(t, dir, c.opts)
		m, err := tools.Probe(ctx(t), file)
		if err != nil {
			t.Fatalf("%s: %v", c.opts.Name, err)
		}
		if m.Codec != c.codec {
			t.Errorf("%s: codec %s != %s", c.opts.Name, m.Codec, c.codec)
		}
		if m.DurationMs < 4900 || m.DurationMs > 5200 {
			t.Errorf("%s: duration %d", c.opts.Name, m.DurationMs)
		}
		want := c.opts.Channels
		if want == 0 {
			want = 2
		}
		if m.Channels != want {
			t.Errorf("%s: channels %d != %d", c.opts.Name, m.Channels, want)
		}
		if c.opts.Title != "" && (m.Title != c.opts.Title || m.Artist != c.opts.Artist) {
			t.Errorf("%s: tags %q %q", c.opts.Name, m.Title, m.Artist)
		}
		if m.Size == 0 || m.SampleRate == 0 {
			t.Errorf("%s: size %d rate %d", c.opts.Name, m.Size, m.SampleRate)
		}
	}

	// garbage and empty files
	bad := filepath.Join(dir, "bad.mp3")
	os.WriteFile(bad, bytes.Repeat([]byte("not audio "), 100), 0o644)
	if _, err := tools.Probe(ctx(t), bad); err == nil {
		t.Error("garbage must fail")
	}
	empty := filepath.Join(dir, "empty.wav")
	os.WriteFile(empty, nil, 0o644)
	if _, err := tools.Probe(ctx(t), empty); err == nil {
		t.Error("empty must fail")
	}
}

func TestAnalyzeAndGain(t *testing.T) {
	testaudio.Skip(t)
	dir := t.TempDir()
	tools := bins

	// quiet: -20 dB sine → about -23 LUFS, gain must be positive
	quiet := testaudio.Generate(t, dir, testaudio.Opts{Name: "quiet.wav", VolumeDB: -20, Lead: 3})
	pcm := filepath.Join(dir, "pcm.raw")
	a, err := tools.Analyze(ctx(t), quiet, AnalyzeOptions{Quality: true, PCMPath: pcm, SilenceDB: -60, SilenceMin: 2, DurationSec: 8})
	if err != nil {
		t.Fatal(err)
	}
	if a.Loudness.Integrated > -18 || a.Loudness.Integrated < -30 {
		t.Errorf("quiet integrated %.1f", a.Loudness.Integrated)
	}
	if a.Loudness.TruePeak > -15 || a.Loudness.TruePeak < -25 {
		t.Errorf("quiet true peak %.1f", a.Loudness.TruePeak)
	}
	gain, limited := ComputeGain(a.Loudness, -14, -1.5)
	if gain <= 0 || limited {
		t.Errorf("quiet gain %.2f limited %v", gain, limited)
	}
	if math.Abs(a.Loudness.Integrated+gain+14) > 0.01 {
		t.Errorf("gain does not reach target: %.2f + %.2f", a.Loudness.Integrated, gain)
	}
	if a.Quality.LeadingSilence < 2.8 {
		t.Errorf("leading silence %.2f, want ~3s: %+v", a.Quality.LeadingSilence, a.Quality.Silences)
	}
	if info, err := os.Stat(pcm); err != nil || info.Size() < 44100*4*5 {
		t.Errorf("pcm missing or short: %v", err)
	}
	if a.Quality.Clipping {
		t.Error("quiet file flagged as clipping")
	}

	// loud: full-scale sine → true peak ~0 dBTP, gain must be limited by the ceiling
	loud := testaudio.Generate(t, dir, testaudio.Opts{Name: "loud.wav", VolumeDB: 0})
	b, err := tools.Analyze(ctx(t), loud, AnalyzeOptions{Quality: true, SilenceDB: -60, SilenceMin: 2, DurationSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	if b.Loudness.TruePeak < -4 {
		t.Errorf("loud true peak %.1f", b.Loudness.TruePeak)
	}
	gain, limited = ComputeGain(b.Loudness, -14, -1.5)
	if gain >= 0 {
		t.Errorf("loud gain %.2f must be negative", gain)
	}
	if b.Loudness.TruePeak+gain > -1.5+0.01 {
		t.Errorf("true peak after gain %.2f above ceiling", b.Loudness.TruePeak+gain)
	}
	_ = limited

	// output measurement of a playlist works the same way
	hls := filepath.Join(dir, "hls")
	if err := tools.EncodeHLS(ctx(t), loud, hls, EncodeOptions{GainDB: gain, Bitrates: []int{128}, SampleRate: 44100, Channels: 2, SegmentSeconds: 6}); err != nil {
		t.Fatal(err)
	}
	out, err := tools.Analyze(ctx(t), filepath.Join(hls, "128", PlaylistName), AnalyzeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Loudness.TruePeak > -1.5+0.6 {
		t.Errorf("encoded true peak %.2f exceeds ceiling", out.Loudness.TruePeak)
	}
	if math.Abs(out.Loudness.Integrated-(b.Loudness.Integrated+gain)) > 1 {
		t.Errorf("encoded loudness %.1f, expected %.1f", out.Loudness.Integrated, b.Loudness.Integrated+gain)
	}
}

func TestComputeGain(t *testing.T) {
	cases := []struct {
		in      Loudness
		gain    float64
		limited bool
	}{
		{Loudness{Integrated: -24, TruePeak: -12}, 10, false},
		{Loudness{Integrated: -24, TruePeak: -5}, 3.5, true},
		{Loudness{Integrated: -8, TruePeak: 0}, -6, false},
		{Loudness{Integrated: LoudnessFloor, TruePeak: LevelFloor}, 0, false},
		{Loudness{Integrated: -70, TruePeak: -60}, 0, false},
	}
	for _, c := range cases {
		g, l := ComputeGain(c.in, -14, -1.5)
		if g != c.gain || l != c.limited {
			t.Errorf("%+v: gain %.2f limited %v, want %.2f %v", c.in, g, l, c.gain, c.limited)
		}
	}
}

func TestParseEBUR128(t *testing.T) {
	stderr := []byte(`[Parsed_ebur128_0 @ 0x1] Summary:

  Integrated loudness:
    I:         -23.4 LUFS
    Threshold: -33.5 LUFS

  Loudness range:
    LRA:         6.2 LU
    Threshold: -43.6 LUFS
    LRA low:   -27.0 LUFS
    LRA high:  -20.8 LUFS

  Sample peak:
    Peak:       -3.1 dBFS

  True peak:
    Peak:       -2.9 dBFS
`)
	l, err := parseEBUR128(stderr)
	if err != nil {
		t.Fatal(err)
	}
	if l.Integrated != -23.4 || l.Threshold != -33.5 || l.LRA != 6.2 || l.SamplePeak != -3.1 || l.TruePeak != -2.9 {
		t.Errorf("%+v", l)
	}
	if _, err := parseEBUR128([]byte("nothing")); err == nil {
		t.Error("missing summary must fail")
	}

	// digital silence: ffmpeg prints -inf peaks, they become the floor
	silent := []byte(`[Parsed_ebur128_0 @ 0x1] Summary:

  Integrated loudness:
    I:         -inf LUFS
    Threshold:  nan LUFS

  Loudness range:
    LRA:        -inf LU
    Threshold:   0.0 LUFS

  Sample peak:
    Peak:       -inf dBFS

  True peak:
    Peak:       -inf dBFS
`)
	l, err = parseEBUR128(silent)
	if err != nil {
		t.Fatal(err)
	}
	if l.Integrated != LoudnessFloor || l.Threshold != LoudnessFloor || l.LRA != 0 || l.SamplePeak != LevelFloor || l.TruePeak != LevelFloor {
		t.Errorf("silent %+v", l)
	}
	if _, err := json.Marshal(l); err != nil {
		t.Errorf("silent loudness json: %v", err)
	}
}

func TestParseQuality(t *testing.T) {
	stderr := []byte(`[silencedetect @ 0x1] silence_start: 0
[silencedetect @ 0x1] silence_end: 2.5 | silence_duration: 2.5
[Parsed_astats_1 @ 0x2] Channel: 1
[Parsed_astats_1 @ 0x2] DC offset: 0.000100
[Parsed_astats_1 @ 0x2] Peak level dB: -0.000000
[Parsed_astats_1 @ 0x2] RMS level dB: -10.000000
[Parsed_astats_1 @ 0x2] Flat factor: 1.500000
[Parsed_astats_1 @ 0x2] Channel: 2
[Parsed_astats_1 @ 0x2] DC offset: 0.000100
[Parsed_astats_1 @ 0x2] Peak level dB: -0.000000
[Parsed_astats_1 @ 0x2] RMS level dB: -20.000000
[Parsed_astats_1 @ 0x2] Flat factor: 1.500000
[Parsed_astats_1 @ 0x2] Overall
[Parsed_astats_1 @ 0x2] DC offset: 0.000100
[Parsed_astats_1 @ 0x2] Peak level dB: -0.000000
[Parsed_astats_1 @ 0x2] RMS level dB: -13.000000
[Parsed_astats_1 @ 0x2] Flat factor: 1.500000
[silencedetect @ 0x1] silence_start: 8
`)
	q, rms := parseQuality(stderr, 10)
	if rms != -13 {
		t.Errorf("rms %.1f", rms)
	}
	if !q.Clipping || q.FlatFactor != 1.5 {
		t.Errorf("clipping %v, flat factor %.1f", q.Clipping, q.FlatFactor)
	}
	if q.LeadingSilence != 2.5 || q.TrailingSilence != 2 {
		t.Errorf("silence lead %.1f trail %.1f: %+v", q.LeadingSilence, q.TrailingSilence, q.Silences)
	}
	if q.ChannelImbalance != 10 || len(q.ChannelRMS) != 2 {
		t.Errorf("imbalance %.1f %v", q.ChannelImbalance, q.ChannelRMS)
	}

	// a digitally silent right channel: -inf levels become the floor, the
	// imbalance is finite and the largest possible
	stderr = []byte(`[Parsed_astats_1 @ 0x2] Channel: 1
[Parsed_astats_1 @ 0x2] Peak level dB: -18.063656
[Parsed_astats_1 @ 0x2] RMS level dB: -21.000000
[Parsed_astats_1 @ 0x2] Flat factor: 0.000000
[Parsed_astats_1 @ 0x2] Channel: 2
[Parsed_astats_1 @ 0x2] Peak level dB: -inf
[Parsed_astats_1 @ 0x2] RMS level dB: -inf
[Parsed_astats_1 @ 0x2] Flat factor: -inf
[Parsed_astats_1 @ 0x2] Overall
[Parsed_astats_1 @ 0x2] Peak level dB: -inf
[Parsed_astats_1 @ 0x2] RMS level dB: -inf
[Parsed_astats_1 @ 0x2] Flat factor: -inf
`)
	q, rms = parseQuality(stderr, 10)
	if rms != LevelFloor || q.FlatFactor != LevelFloor || q.ChannelRMS[1] != LevelFloor || q.ChannelImbalance != -21-LevelFloor || q.Clipping {
		t.Errorf("silent channel: rms %.1f %+v", rms, q)
	}
	if _, err := json.Marshal(q); err != nil {
		t.Errorf("silent channel quality json: %v", err)
	}
}

// TestEncodeAndVerify — one run gives every rendition in its folder:
// verified, aac 44.1 kHz stereo (a mono source too), audio only, cut at
// the same points, ~6 s segments
func TestEncodeAndVerify(t *testing.T) {
	testaudio.Skip(t)
	dir := t.TempDir()
	tools := bins
	bitrates := []int{128, 64, 256}
	for _, o := range []testaudio.Opts{
		{Name: "s.mp3", Seconds: 13},
		{Name: "s.flac", Seconds: 13, Channels: 1, Rate: 48000},
		{Name: "low.wav", Seconds: 13, Rate: 22050},
		{Name: "short.wav", Seconds: 1.5},
		{Name: "long.wav", Seconds: 65},
		{Name: "art.mp3", Seconds: 13, Cover: true},
		{Name: "art.flac", Seconds: 13, Cover: true},
	} {
		src := testaudio.Generate(t, dir, o)
		// embedded art is a second stream of the source
		nb := "1"
		if o.Cover {
			nb = "2"
		}
		streams, _, err := tools.run(ctx(t), "", tools.FFprobe, "-v", "error", "-show_entries", "format=nb_streams", "-of", "csv=p=0", src)
		if err != nil || strings.TrimSpace(string(streams)) != nb {
			t.Fatalf("%s: source has %q streams, want %s: %v", o.Name, streams, nb, err)
		}
		hls := filepath.Join(dir, o.Name+".hls")
		if err := tools.EncodeHLS(ctx(t), src, hls, EncodeOptions{GainDB: -3, Bitrates: bitrates, SampleRate: 44100, Channels: 2, SegmentSeconds: 6}); err != nil {
			t.Fatalf("%s: %v", o.Name, err)
		}
		if entries, _ := os.ReadDir(hls); len(entries) != len(bitrates) {
			t.Errorf("%s: %d entries in the hls dir, want a folder per rendition", o.Name, len(entries))
		}
		var first Playlist
		for i, kbit := range bitrates {
			rendition := filepath.Join(hls, strconv.Itoa(kbit))
			p, err := tools.VerifyHLS(ctx(t), rendition, o.Seconds)
			if err != nil {
				t.Fatalf("%s %d: %v", o.Name, kbit, err)
			}
			if p.Init != InitName || !p.VOD || !p.EndList {
				t.Errorf("%s %d: playlist %+v", o.Name, kbit, p)
			}
			want := int(math.Ceil(o.Seconds / 6))
			if n := len(p.Segments); n < want-1 || n > want+1 {
				t.Errorf("%s %d: %d segments for %.0fs, want ~%d", o.Name, kbit, n, o.Seconds, want)
			}
			for j, s := range p.Segments {
				if s.Name != "seg_"+pad4(j)+".m4s" {
					t.Errorf("%s %d: segment %d named %s", o.Name, kbit, j, s.Name)
				}
				if j < len(p.Segments)-1 && math.Abs(s.Duration-6) > 0.1 {
					t.Errorf("%s %d: segment %d is %.3fs", o.Name, kbit, j, s.Duration)
				}
			}
			// every rendition is cut exactly like the first
			if i == 0 {
				first = p
			} else if !slices.Equal(p.Segments, first.Segments) {
				t.Errorf("%s: segments of %d %+v differ from %d %+v", o.Name, kbit, p.Segments, bitrates[0], first.Segments)
			}
			m, err := tools.Probe(ctx(t), filepath.Join(rendition, PlaylistName))
			if err != nil {
				t.Fatalf("%s %d: probe hls: %v", o.Name, kbit, err)
			}
			if m.Codec != "aac" || m.SampleRate != 44100 || m.Channels != 2 {
				t.Errorf("%s %d: hls stream %s %d %d", o.Name, kbit, m.Codec, m.SampleRate, m.Channels)
			}
			// audio only, embedded art never reaches the stream
			streams, _, err = tools.run(ctx(t), rendition, tools.FFprobe, "-v", "error", "-show_entries", "format=nb_streams", "-of", "csv=p=0", PlaylistName)
			if err != nil || strings.TrimSpace(string(streams)) != "1" {
				t.Errorf("%s %d: hls has %q streams: %v", o.Name, kbit, streams, err)
			}
		}
	}
}

// TestBandwidth — the peak is the densest segment, the average all bytes
// over the whole duration, both in bit/s rounded up
func TestBandwidth(t *testing.T) {
	p := Playlist{Segments: []Segment{{"a", 6}, {"b", 6}, {"c", 2}}, Duration: 14}
	peak, average := p.Bandwidth(map[string]int64{"a": 96000, "b": 97500, "c": 33001})
	if peak != 132004 || average != 129430 {
		t.Errorf("peak %d average %d", peak, average)
	}
}

// TestMaster — header, then the variants in the given order with their
// bandwidths, the AAC-LC codec and relative uris
func TestMaster(t *testing.T) {
	got := Master([]Variant{{"128/index.m3u8", 140000, 131000}, {"64/index.m3u8", 70000, 66000}})
	want := "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-INDEPENDENT-SEGMENTS\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=140000,AVERAGE-BANDWIDTH=131000,CODECS=\"mp4a.40.2\"\n128/index.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=70000,AVERAGE-BANDWIDTH=66000,CODECS=\"mp4a.40.2\"\n64/index.m3u8\n"
	if got != want {
		t.Errorf("master:\n%s", got)
	}
}

func pad4(i int) string {
	s := "000" + string(rune('0'+i%10))
	if i >= 10 {
		s = "00" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return s
}

func TestParsePlaylist(t *testing.T) {
	good := "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4.000000,\nseg_0000.m4s\n#EXTINF:1.500000,\nseg_0001.m4s\n#EXT-X-ENDLIST\n"
	p, err := ParsePlaylist(good)
	if err != nil {
		t.Fatal(err)
	}
	if p.Init != "init.mp4" || len(p.Segments) != 2 || math.Abs(p.Duration-5.5) > 1e-6 || !p.EndList || !p.VOD || !p.Independent {
		t.Errorf("%+v", p)
	}
	if _, err := ParsePlaylist("garbage"); err == nil {
		t.Error("bad header must fail")
	}
}

func TestWaveform(t *testing.T) {
	// 2 seconds at 1000 Hz, 10 points per second → bucket of 100 samples
	var buf bytes.Buffer
	for i := 0; i < 2000; i++ {
		v := float32(0)
		switch {
		case i < 100: // first bucket: ramp -0.5..0.5
			v = float32(i)/100 - 0.5
		case i >= 1900: // last bucket: full scale
			v = 0.9
		}
		binary.Write(&buf, binary.LittleEndian, math.Float32bits(v))
	}
	w, err := BuildWaveform(&buf, 1000, 10, 6) // +6 dB ≈ ×2
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Min) != 20 || len(w.Max) != 20 || len(w.RMS) != 20 || w.DurationMs != 2000 || w.SampleRate != 10 {
		t.Fatalf("shape: %d %d %d dur %d rate %d", len(w.Min), len(w.Max), len(w.RMS), w.DurationMs, w.SampleRate)
	}
	if w.Min[0] > -0.99 || w.Max[0] < 0.97 {
		t.Errorf("first bucket after gain min %.2f max %.2f", w.Min[0], w.Max[0])
	}
	if w.Max[19] != 1 || w.RMS[19] != 1 { // clamped
		t.Errorf("last bucket max %.2f rms %.2f", w.Max[19], w.RMS[19])
	}
	if w.Min[5] != 0 || w.Max[5] != 0 || w.RMS[5] != 0 {
		t.Errorf("silent bucket %v %v %v", w.Min[5], w.Max[5], w.RMS[5])
	}

	d := w.Downsample(4)
	if len(d.Min) != 4 || d.Max[3] != 1 || d.Min[0] != w.Min[0] {
		t.Errorf("downsample %+v", d)
	}

	if _, err := BuildWaveform(bytes.NewReader(nil), 1000, 10, 0); err == nil {
		t.Error("empty pcm must fail")
	}
}

func TestPeaks(t *testing.T) {
	// 1024 buckets: 2 per point; a negative spike in bucket 201, full scale
	// from bucket 1000, a quiet bucket 0
	n := 1024
	w := Waveform{Min: make([]float32, n), Max: make([]float32, n), RMS: make([]float32, n)}
	w.Min[0], w.Max[0] = -0.1, 0.2
	w.Min[201] = -0.5
	for i := 1000; i < n; i++ {
		w.Min[i], w.Max[i] = -1, 1
	}
	p := w.Peaks(512)
	if len(p) != 512 {
		t.Fatalf("%d points", len(p))
	}
	if p[0] != 51 || p[100] != 128 || p[499] != 0 || p[500] != 255 || p[511] != 255 {
		t.Errorf("peaks %v", p)
	}
	for i, v := range p {
		if v != 0 && i != 0 && i != 100 && i < 500 {
			t.Errorf("point %d = %d", i, v)
		}
	}

	// fewer buckets than points: each point takes bucket i*n/512
	short := Waveform{Min: []float32{0, -1, 0}, Max: []float32{0.5, 0, 0}, RMS: make([]float32, 3)}
	p = short.Peaks(512)
	if len(p) != 512 || p[0] != 128 || p[170] != 128 || p[171] != 255 || p[341] != 255 || p[342] != 0 || p[511] != 0 {
		t.Errorf("3 buckets: %v", p)
	}

	// silence and nothing: zeros
	if p := (Waveform{Min: make([]float32, 5000), Max: make([]float32, 5000)}).Peaks(512); !bytes.Equal(p, make([]byte, 512)) {
		t.Errorf("silence %v", p)
	}
	if p := (Waveform{}).Peaks(512); !bytes.Equal(p, make([]byte, 512)) {
		t.Errorf("empty %v", p)
	}
}

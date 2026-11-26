package audio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EncodeOptions — AAC-LC renditions inside fragmented MP4 HLS VOD. Every
// rendition has the same gain, sample rate, channels and segment length;
// only the bitrate differs.
type EncodeOptions struct {
	GainDB         float64 // linear gain applied before encoding
	Bitrates       []int   // kbit/s, one rendition each, at least one
	SampleRate     int     // Hz
	Channels       int     // channel count
	SegmentSeconds int     // hls segment
}

// Output names inside the folder of a rendition.
const (
	PlaylistName = "index.m3u8"
	InitName     = "init.mp4"
	SegmentName  = "seg_%04d.m4s"
)

// codecs — the CODECS attribute of AAC-LC in a master playlist.
const codecs = "mp4a.40.2"

// EncodeHLS writes one rendition per bitrate into dir/<kbit>/:
// index.m3u8, init.mp4 and seg_NNNN.m4s, the paths in the playlists
// relative. One ffmpeg run: the first audio stream is decoded, gained,
// resampled and converted to the channel count once, then split into one
// AAC encoder and HLS muxer per bitrate, so every rendition is cut at the
// same points. Video, cover art and subtitles are dropped.
func (t Tools) EncodeHLS(ctx context.Context, in, dir string, o EncodeOptions) error {
	abs, err := filepath.Abs(in)
	if err != nil {
		return err
	}
	// "<n>c" is the default layout of n channels, as -ac n picks it
	graph := fmt.Sprintf("[0:a:0]volume=%.2fdB,aformat=sample_rates=%d:channel_layouts=%dc,asplit=%d", o.GainDB, o.SampleRate, o.Channels, len(o.Bitrates))
	for i := range o.Bitrates {
		graph += fmt.Sprintf("[a%d]", i)
	}
	args := []string{
		"-nostdin", "-hide_banner", "-nostats", "-loglevel", "error", "-y",
		"-i", abs,
		"-filter_complex", graph,
	}
	for i, kbit := range o.Bitrates {
		folder := strconv.Itoa(kbit)
		if err := os.MkdirAll(filepath.Join(dir, folder), 0o755); err != nil {
			return err
		}
		args = append(args,
			"-map", fmt.Sprintf("[a%d]", i), "-map_metadata", "-1",
			"-c:a", "aac", "-profile:a", "aac_low", "-b:a", folder+"k",
			"-f", "hls",
			"-hls_time", strconv.Itoa(o.SegmentSeconds),
			"-hls_playlist_type", "vod",
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", InitName,
			"-hls_segment_filename", filepath.Join(folder, SegmentName),
			"-hls_flags", "independent_segments",
			"-hls_list_size", "0",
			filepath.Join(folder, PlaylistName),
		)
	}
	_, _, err = t.run(ctx, dir, t.FFmpeg, args...)
	return err
}

// Variant — one rendition in a master playlist.
type Variant struct {
	URI              string // media playlist, relative to the master
	Bandwidth        int    // peak segment bit rate, bit/s
	AverageBandwidth int    // bit/s
}

// Master — the master playlist of AAC-LC fMP4 variants in their order:
// players start with the first one.
func Master(variants []Variant) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	for _, v := range variants {
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,CODECS=\"%s\"\n%s\n", v.Bandwidth, v.AverageBandwidth, codecs, v.URI)
	}
	return b.String()
}

// Playlist — parsed HLS VOD playlist.
type Playlist struct {
	Version     int
	VOD         bool
	EndList     bool
	Independent bool
	Init        string
	Segments    []Segment
	Duration    float64 // seconds, sum of EXTINF
}

// Segment of a playlist.
type Segment struct {
	Name     string
	Duration float64
}

// ErrPlaylist — playlist does not meet the requirements.
var ErrPlaylist = errors.New("audio: invalid playlist")

// ParsePlaylist — minimal m3u8 parser for the files ffmpeg writes.
func ParsePlaylist(text string) (p Playlist, err error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return p, fmt.Errorf("%w: missing #EXTM3U", ErrPlaylist)
	}
	var dur float64
	haveInf := false
	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXT-X-VERSION:"):
			p.Version, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-VERSION:"))
		case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:"):
			p.VOD = strings.EqualFold(strings.TrimPrefix(line, "#EXT-X-PLAYLIST-TYPE:"), "VOD")
		case line == "#EXT-X-ENDLIST":
			p.EndList = true
		case line == "#EXT-X-INDEPENDENT-SEGMENTS":
			p.Independent = true
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			for _, attr := range strings.Split(strings.TrimPrefix(line, "#EXT-X-MAP:"), ",") {
				if k, v, ok := strings.Cut(attr, "="); ok && strings.TrimSpace(k) == "URI" {
					p.Init = strings.Trim(strings.TrimSpace(v), `"`)
				}
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			v, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			dur, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
			haveInf = true
		case strings.HasPrefix(line, "#"):
		default:
			if !haveInf {
				return p, fmt.Errorf("%w: segment %s without #EXTINF", ErrPlaylist, line)
			}
			p.Segments = append(p.Segments, Segment{Name: line, Duration: dur})
			p.Duration += dur
			haveInf = false
		}
	}
	return p, nil
}

// Bandwidth — peak and average bit rate of the segments in bit/s, from
// their sizes in bytes by name: the peak is the rate of the densest
// segment, the average all bytes over the whole duration; both rounded up.
// Segments without a duration count for the average only.
func (p Playlist) Bandwidth(sizes map[string]int64) (peak, average int) {
	var total int64
	for _, s := range p.Segments {
		total += sizes[s.Name]
		if s.Duration > 0 {
			peak = max(peak, int(math.Ceil(float64(sizes[s.Name])*8/s.Duration)))
		}
	}
	if p.Duration > 0 {
		average = int(math.Ceil(float64(total) * 8 / p.Duration))
	}
	return peak, average
}

// VerifyHLS checks the media playlist of one rendition in dir: VOD,
// ENDLIST, EXT-X-MAP with a relative init, every segment present and
// non-empty, total duration within tolerance of expectedSec, and the whole
// stream decodes without errors. Returns the parsed playlist.
func (t Tools) VerifyHLS(ctx context.Context, dir string, expectedSec float64) (p Playlist, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, PlaylistName))
	if err != nil {
		return p, fmt.Errorf("%w: %v", ErrPlaylist, err)
	}
	if p, err = ParsePlaylist(string(raw)); err != nil {
		return
	}
	switch {
	case !p.VOD:
		return p, fmt.Errorf("%w: not VOD", ErrPlaylist)
	case !p.EndList:
		return p, fmt.Errorf("%w: no EXT-X-ENDLIST", ErrPlaylist)
	case p.Init == "":
		return p, fmt.Errorf("%w: no EXT-X-MAP", ErrPlaylist)
	case !relative(p.Init):
		return p, fmt.Errorf("%w: init path is not relative", ErrPlaylist)
	case len(p.Segments) == 0:
		return p, fmt.Errorf("%w: no segments", ErrPlaylist)
	}
	for _, name := range append([]string{p.Init}, segmentNames(p)...) {
		if !relative(name) {
			return p, fmt.Errorf("%w: %s is not relative", ErrPlaylist, name)
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() == 0 {
			return p, fmt.Errorf("%w: %s missing or empty", ErrPlaylist, name)
		}
	}
	if expectedSec > 0 {
		tol := math.Max(1.5, expectedSec*0.02)
		if math.Abs(p.Duration-expectedSec) > tol {
			return p, fmt.Errorf("%w: duration %.2fs, expected %.2fs", ErrPlaylist, p.Duration, expectedSec)
		}
	}

	// decode the whole stream: any decoder error fails verification
	_, stderr, err := t.run(ctx, dir, t.FFmpeg, "-nostdin", "-hide_banner", "-nostats", "-loglevel", "error",
		"-i", PlaylistName, "-f", "null", "-")
	if err != nil {
		return p, fmt.Errorf("%w: decode: %v", ErrPlaylist, err)
	}
	if s := strings.TrimSpace(string(stderr)); s != "" {
		return p, fmt.Errorf("%w: decode: %s", ErrPlaylist, tail(s, 512))
	}
	return p, nil
}

func segmentNames(p Playlist) []string {
	res := make([]string, 0, len(p.Segments))
	for _, s := range p.Segments {
		res = append(res, s.Name)
	}
	return res
}

func relative(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && !strings.Contains(name, "://") && !strings.Contains(name, "..")
}

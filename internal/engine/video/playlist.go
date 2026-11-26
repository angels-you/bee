package video

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Playlist — an HLS VOD media playlist as ffmpeg writes it.
type Playlist struct {
	Version     int
	VOD         bool
	EndList     bool
	Independent bool
	Init        string    // the EXT-X-MAP uri: init.mp4 of fMP4, empty for MPEG-TS
	Segments    []Segment // in order
	Duration    float64   // seconds, the sum of EXTINF
}

// Segment of a media playlist.
type Segment struct {
	Name     string // uri, relative to the playlist
	Duration float64
}

// Parse — the playlist text: the tags bee checks and every segment with
// its EXTINF duration. Unknown tags are skipped.
func (a *Playlist) Parse(text string) error {
	// the lines, the header first
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return errors.New("video: playlist: no #EXTM3U")
	}

	// the EXTINF waiting for its uri line; -1 = none
	extinf := -1.0
	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)

		switch {
		case line == "":
			continue

		case strings.HasPrefix(line, "#EXT-X-VERSION:"):
			a.Version, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-VERSION:"))

		case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:"):
			a.VOD = strings.TrimPrefix(line, "#EXT-X-PLAYLIST-TYPE:") == "VOD"

		case line == "#EXT-X-ENDLIST":
			a.EndList = true

		case line == "#EXT-X-INDEPENDENT-SEGMENTS":
			a.Independent = true

		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			_, uri, ok := strings.Cut(line, `URI="`)
			if !ok {
				return errors.New("video: playlist: EXT-X-MAP without URI")
			}
			a.Init, _, _ = strings.Cut(uri, `"`)

		case strings.HasPrefix(line, "#EXTINF:"):
			value, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			d, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("video: playlist: EXTINF %q", value)
			}
			extinf = d

		case strings.HasPrefix(line, "#"):
			continue

		default:
			// a uri line is the segment of the EXTINF above it
			if extinf < 0 {
				return fmt.Errorf("video: playlist: segment %s without EXTINF", line)
			}
			a.Segments = append(a.Segments, Segment{Name: line, Duration: extinf})
			a.Duration += extinf
			extinf = -1
		}
	}
	return nil
}

// Bandwidth — the peak and the average bit rate of the segments in bit/s
// from their sizes in bytes by name (as published: after encryption): the
// peak is the rate of the densest segment, the average all bytes over the
// whole duration; both rounded up. The init is not counted.
func (a *Playlist) Bandwidth(sizes map[string]int64) (peak, average int) {
	// all bytes, and the peak of the densest segment
	var total int64
	for _, s := range a.Segments {
		total += sizes[s.Name]
		if s.Duration > 0 {
			rate := int(math.Ceil(float64(sizes[s.Name]) * 8 / s.Duration))
			peak = max(peak, rate)
		}
	}

	// the average over the whole duration
	if a.Duration > 0 {
		average = int(math.Ceil(float64(total) * 8 / a.Duration))
	}
	return peak, average
}

// Master — the master playlist of an adaptive Stream: one variant per
// rendition, in profile order (players start with the first).
type Master struct {
	Version  int // the EXT-X-VERSION of the media playlists
	Variants []Variant
}

// Variant — one rendition in the master playlist.
type Variant struct {
	URI              string  // the media playlist, relative to the master: 720p/index.m3u8
	Bandwidth        int     // peak segment bit rate, bit/s
	AverageBandwidth int     // bit/s
	Width            int     // measured
	Height           int     // measured
	FPS              float64 // measured
	Codecs           string  // RFC 6381, measured: avc1.64001f,mp4a.40.2
}

// String — the master playlist text.
func (a *Master) String() string {
	var b strings.Builder

	// the header
	b.WriteString("#EXTM3U\n")
	fmt.Fprintf(&b, "#EXT-X-VERSION:%d\n", a.Version)
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")

	// one variant per rendition: its attributes, then its uri
	for _, v := range a.Variants {
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,RESOLUTION=%dx%d,FRAME-RATE=%.3f,CODECS=\"%s\"\n",
			v.Bandwidth, v.AverageBandwidth, v.Width, v.Height, v.FPS, v.Codecs)
		b.WriteString(v.URI + "\n")
	}
	return b.String()
}

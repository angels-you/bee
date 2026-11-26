package audio

import (
	"context"
	"encoding/json"
	"fmt"
	types "github.com/angels-you/bee/pkg/types/audio"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Metadata is the source data and tags: public job type
type Metadata = types.Metadata

// ffprobe -print_format json
type probeOutput struct {
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		Size       string            `json:"size"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType        string            `json:"codec_type"`
		CodecName        string            `json:"codec_name"`
		SampleRate       string            `json:"sample_rate"`
		Channels         int               `json:"channels"`
		ChannelLayout    string            `json:"channel_layout"`
		BitsPerSample    int               `json:"bits_per_sample"`
		BitsPerRawSample string            `json:"bits_per_raw_sample"`
		BitRate          string            `json:"bit_rate"`
		Duration         string            `json:"duration"`
		Tags             map[string]string `json:"tags"`
	} `json:"streams"`
	Error struct {
		String string `json:"string"`
	} `json:"error"`
}

const (
	maxTags     = 64
	maxTagRunes = 1024
)

// Probe — metadata of a local file. A file without an audio stream is
// ErrNoAudio, a file ffprobe cannot read is ErrInvalid.
func (t Tools) Probe(ctx context.Context, file string) (m Metadata, err error) {
	out, _, err := t.run(ctx, "", t.FFprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", "-show_error", file)
	if err != nil {
		if ctx.Err() != nil {
			return m, err
		}
		return m, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	var p probeOutput
	if err = json.Unmarshal(out, &p); err != nil {
		return m, fmt.Errorf("%w: ffprobe output: %v", ErrInvalid, err)
	}
	if p.Error.String != "" {
		return m, fmt.Errorf("%w: %s", ErrInvalid, p.Error.String)
	}

	// the first audio stream; video and attached pictures are ignored
	audio := -1
	for i, s := range p.Streams {
		if s.CodecType == "audio" {
			audio = i
			break
		}
	}
	if audio < 0 {
		return m, ErrNoAudio
	}
	s := p.Streams[audio]

	m.Format = p.Format.FormatName
	m.Codec = s.CodecName
	m.Size = atoi64(p.Format.Size)
	m.SampleRate = int(atoi64(s.SampleRate))
	m.Channels = s.Channels
	m.ChannelLayout = s.ChannelLayout
	m.BitDepth = s.BitsPerSample
	if m.BitDepth == 0 {
		m.BitDepth = int(atoi64(s.BitsPerRawSample))
	}
	m.Bitrate = int(atoi64(p.Format.BitRate))
	if m.Bitrate == 0 {
		m.Bitrate = int(atoi64(s.BitRate))
	}

	dur := atof(p.Format.Duration)
	if dur <= 0 {
		dur = atof(s.Duration)
	}
	if dur > 0 && !math.IsInf(dur, 0) && !math.IsNaN(dur) {
		m.DurationMs = int64(math.Round(dur * 1000))
	}
	if m.DurationMs <= 0 || m.SampleRate <= 0 || m.Channels <= 0 {
		return m, fmt.Errorf("%w: no duration, sample rate or channels", ErrInvalid)
	}

	m.Tags = mergeTags(p.Format.Tags, s.Tags)
	t.fillFromTags(&m)
	return m, nil
}

// mergeTags — format tags first, stream tags (ogg/opus vorbis comments)
// fill what is missing; keys lowercase, limited size.
func mergeTags(groups ...map[string]string) map[string]string {
	res := map[string]string{}
	for _, g := range groups {
		keys := make([]string, 0, len(g))
		for k := range g {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lk := strings.ToLower(strings.TrimSpace(k))
			if lk == "" || len(lk) > 64 {
				continue
			}
			if _, ok := res[lk]; ok {
				continue
			}
			if len(res) >= maxTags {
				break
			}
			v := strings.TrimSpace(g[k])
			if r := []rune(v); len(r) > maxTagRunes {
				v = string(r[:maxTagRunes])
			}
			if v != "" {
				res[lk] = v
			}
		}
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

// Supported — the source codec (ffprobe codec_name) is one the pipeline
// encodes from. A fixed property of bee, not a setting.
func (t Tools) Supported(m *Metadata) bool {
	switch strings.ToLower(m.Codec) {
	case "mp3", "flac", "pcm_s16le", "pcm_s24le", "pcm_s32le", "pcm_f32le", "pcm_s16be", "pcm_s24be", "pcm_f32be", "pcm_u8", "aac", "alac", "vorbis", "opus", "wmav2", "mp2", "ac3":
		return true
	}
	return false
}

// fillFromTags — normalized fields from the common tag names of ID3,
// vorbis comments and mp4 atoms as ffprobe reports them.
func (t Tools) fillFromTags(m *Metadata) {
	get := func(names ...string) string {
		for _, n := range names {
			if v, ok := m.Tags[n]; ok {
				return v
			}
		}
		return ""
	}
	m.Title = get("title", "tit2")
	m.Artist = get("artist", "tpe1")
	m.Album = get("album", "talb")
	m.AlbumArtist = get("album_artist", "albumartist", "album artist", "tpe2")
	m.Composer = get("composer", "tcom")
	m.Genre = get("genre", "tcon")
	m.Date = get("date", "year", "tdrc", "tyer", "originaldate")
	m.ISRC = strings.ToUpper(strings.ReplaceAll(get("isrc", "tsrc"), "-", ""))
	m.Copyright = get("copyright", "tcop")
	m.Encoder = get("encoder", "encoded_by", "tenc", "tsse")
	m.TrackNumber = firstInt(get("track", "tracknumber", "trck"))
	m.DiscNumber = firstInt(get("disc", "discnumber", "tpos"))
	if len(m.Date) >= 4 {
		if y, err := strconv.Atoi(m.Date[:4]); err == nil && y > 1000 && y < 3000 {
			m.Year = y
		}
	}
}

// firstInt — "3/12" → 3, "" → 0
func firstInt(s string) int {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "/-"); i > 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

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

// Probe — metadata of a local file. A file without an audio stream is
// ErrNoAudio, a file ffprobe cannot read is ErrInvalid.
func (t Tools) Probe(ctx context.Context, file string) (m Metadata, err error) {
	// run ffprobe
	out, _, err := t.run(ctx, "", t.FFprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", "-show_error", file)
	if err != nil {
		if ctx.Err() != nil {
			return m, err
		}
		return m, fmt.Errorf("%w: %v", t.ErrInvalid(), err)
	}

	// parse the json output
	var p probeOutput
	err = json.Unmarshal(out, &p)
	if err != nil {
		return m, fmt.Errorf("%w: ffprobe output: %v", t.ErrInvalid(), err)
	}

	if p.Error.String != "" {
		return m, fmt.Errorf("%w: %s", t.ErrInvalid(), p.Error.String)
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
		return m, t.ErrNoAudio()
	}
	s := p.Streams[audio]

	// stream and format fields
	m.Format = p.Format.FormatName
	m.Codec = s.CodecName
	m.Size = t.atoi64(p.Format.Size)
	m.SampleRate = int(t.atoi64(s.SampleRate))
	m.Channels = s.Channels
	m.ChannelLayout = s.ChannelLayout

	// bit depth: bits per sample, else raw bits
	m.BitDepth = s.BitsPerSample
	if m.BitDepth == 0 {
		m.BitDepth = int(t.atoi64(s.BitsPerRawSample))
	}

	// bitrate: format, else stream
	m.Bitrate = int(t.atoi64(p.Format.BitRate))
	if m.Bitrate == 0 {
		m.Bitrate = int(t.atoi64(s.BitRate))
	}

	// duration: format, else stream; only a finite positive value counts
	dur := t.atof(p.Format.Duration)
	if dur <= 0 {
		dur = t.atof(s.Duration)
	}

	switch {
	case dur <= 0:
	case math.IsInf(dur, 0):
	case math.IsNaN(dur):
	default:
		m.DurationMs = int64(math.Round(dur * 1000))
	}

	// a decodable file has duration, sample rate and channels
	switch {
	case m.DurationMs <= 0:
		return m, fmt.Errorf("%w: no duration, sample rate or channels", t.ErrInvalid())
	case m.SampleRate <= 0:
		return m, fmt.Errorf("%w: no duration, sample rate or channels", t.ErrInvalid())
	case m.Channels <= 0:
		return m, fmt.Errorf("%w: no duration, sample rate or channels", t.ErrInvalid())
	}

	// tags and the fields from them
	m.Tags = t.mergeTags(p.Format.Tags, s.Tags)
	t.fillFromTags(&m)

	return m, nil
}

// mergeTags — format tags first, stream tags (ogg/opus vorbis comments)
// fill what is missing; keys lowercase, limited size.
func (t Tools) mergeTags(groups ...map[string]string) map[string]string {
	// limits: tag count, key length, value length in runes
	maxTags := 64
	maxKey := 64
	maxTagRunes := 1024

	res := map[string]string{}
	for _, g := range groups {
		// sorted keys: the same input gives the same tags
		keys := make([]string, 0, len(g))
		for k := range g {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			// normalized key
			lk := strings.ToLower(strings.TrimSpace(k))
			switch {
			case lk == "":
				continue
			case len(lk) > maxKey:
				continue
			}

			// an earlier group wins
			_, ok := res[lk]
			if ok {
				continue
			}

			if len(res) >= maxTags {
				break
			}

			// trimmed and limited value
			v := strings.TrimSpace(g[k])
			r := []rune(v)
			if len(r) > maxTagRunes {
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
	// text fields
	m.Title = t.tag(m, "title", "tit2")
	m.Artist = t.tag(m, "artist", "tpe1")
	m.Album = t.tag(m, "album", "talb")
	m.AlbumArtist = t.tag(m, "album_artist", "albumartist", "album artist", "tpe2")
	m.Composer = t.tag(m, "composer", "tcom")
	m.Genre = t.tag(m, "genre", "tcon")
	m.Date = t.tag(m, "date", "year", "tdrc", "tyer", "originaldate")
	m.Copyright = t.tag(m, "copyright", "tcop")
	m.Encoder = t.tag(m, "encoder", "encoded_by", "tenc", "tsse")

	// isrc: upper case without dashes
	isrc := t.tag(m, "isrc", "tsrc")
	isrc = strings.ReplaceAll(isrc, "-", "")
	m.ISRC = strings.ToUpper(isrc)

	// track and disc numbers: "3/12" → 3
	m.TrackNumber = t.firstInt(t.tag(m, "track", "tracknumber", "trck"))
	m.DiscNumber = t.firstInt(t.tag(m, "disc", "discnumber", "tpos"))

	// year from the first four characters of the date
	if len(m.Date) >= 4 {
		y, err := strconv.Atoi(m.Date[:4])
		switch {
		case err != nil:
		case y <= 1000:
		case y >= 3000:
		default:
			m.Year = y
		}
	}
}

// tag — the value of the first of names present in the tags, or "".
func (t Tools) tag(m *Metadata, names ...string) string {
	for _, n := range names {
		v, ok := m.Tags[n]
		if ok {
			return v
		}
	}
	return ""
}

// firstInt — "3/12" → 3, "" → 0
func (t Tools) firstInt(s string) int {
	s = strings.TrimSpace(s)

	// the part before the separator
	i := strings.IndexAny(s, "/-")
	if i > 0 {
		s = s[:i]
	}

	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// atoi64 — an integer, 0 when not a number.
func (t Tools) atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// atof — a float, 0 when not a number.
func (t Tools) atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

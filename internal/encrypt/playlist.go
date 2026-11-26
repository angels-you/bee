package encrypt

import (
	"errors"
	"fmt"
	"strings"
)

// Playlist returns the ffmpeg media playlist text where every #EXTINF is preceded by
//
//	#EXT-X-KEY:METHOD=AES-128,URI="<uri>",IV=0x<iv hex>
//
// The IV is taken from ivs by the segment name under #EXTINF. uri is the key address as
// the player sees it (from the job); no quotes or line breaks. Other
// lines stay as they are and in the same order, so #EXT-X-MAP
// (the unencrypted init) stays above the first key
func (a *HLS) Playlist(text, uri string, ivs map[string][]byte) (string, error) {
	switch {
	case uri == "":
		return "", errors.New("encrypt: key uri is empty")
	case strings.ContainsAny(uri, "\"\r\n"):
		return "", errors.New("encrypt: key uri holds a quote or a line break")
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+len(ivs))
	for i, line := range lines {

		// a line not about a segment stays as is
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXTINF:") {
			out = append(out, line)
			continue
		}

		// the segment of this #EXTINF: the first line below, not empty and not a tag
		name := a.segment(lines[i+1:])
		if name == "" {
			return "", errors.New("encrypt: #EXTINF without a segment")
		}

		// its IV
		iv, ok := ivs[name]
		switch {
		case !ok:
			return "", fmt.Errorf("encrypt: no iv for segment %s", name)
		case len(iv) != a.size:
			return "", fmt.Errorf("encrypt: the iv of segment %s is not %d bytes", name, a.size)
		}

		// key line right above #EXTINF
		out = append(out, fmt.Sprintf(`#EXT-X-KEY:METHOD=AES-128,URI="%s",IV=0x%x`, uri, iv))
		out = append(out, line)
	}

	return strings.Join(out, "\n"), nil
}

// segment returns the first segment uri line in lines: not empty and not a tag
func (a *HLS) segment(lines []string) string {
	for _, next := range lines {
		next = strings.TrimSpace(next)

		switch {
		case next == "":
			continue
		case strings.HasPrefix(next, "#"):
			continue
		}

		return next
	}

	return ""
}

package encrypt

import (
	"errors"
	"fmt"
	"strings"
)

// Playlist — текст media playlist ffmpeg, где перед каждым #EXTINF стоит
//
//	#EXT-X-KEY:METHOD=AES-128,URI="<uri>",IV=0x<iv hex>
//
// IV берётся из ivs по имени сегмента под #EXTINF. uri — адрес ключа, как
// его видит плеер (из job); без кавычек и переводов строки. Остальные
// строки остаются как есть и в том же порядке, поэтому #EXT-X-MAP
// (открытый init) остаётся над первым ключом
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

		// строка не про сегмент — как есть
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXTINF:") {
			out = append(out, line)
			continue
		}

		// сегмент этого #EXTINF: первая строка ниже, не пустая и не тег
		name := a.segment(lines[i+1:])
		if name == "" {
			return "", errors.New("encrypt: #EXTINF without a segment")
		}

		// его IV
		iv, ok := ivs[name]
		switch {
		case !ok:
			return "", fmt.Errorf("encrypt: no iv for segment %s", name)
		case len(iv) != a.size:
			return "", fmt.Errorf("encrypt: the iv of segment %s is not %d bytes", name, a.size)
		}

		// строка ключа прямо над #EXTINF
		out = append(out, fmt.Sprintf(`#EXT-X-KEY:METHOD=AES-128,URI="%s",IV=0x%x`, uri, iv))
		out = append(out, line)
	}

	return strings.Join(out, "\n"), nil
}

// segment — первая строка uri сегмента в lines: не пустая и не тег
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

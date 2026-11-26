package encrypt_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/angels-you/bee/internal/encrypt"
)

// fixture — the key and the ffmpeg playlist every test uses.
type fixture struct {
	key      []byte
	playlist string
	keyLine  *regexp.Regexp
}

// init — the fixed test key, a three segment fMP4 media playlist as ffmpeg
// writes it and the key line Playlist must insert for bee://1001.
func (a *fixture) init() {
	a.key = []byte("0123456789abcdef")
	a.playlist = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="init.mp4"
#EXTINF:4.000000,
seg_0000.m4s
#EXTINF:4.000000,
seg_0001.m4s
#EXTINF:1.000000,
seg_0002.m4s
#EXT-X-ENDLIST
`
	a.keyLine = regexp.MustCompile(`^#EXT-X-KEY:METHOD=AES-128,URI="bee://1001",IV=0x([0-9a-f]{32})$`)
}

// decrypt — AES-128-CBC and PKCS7 unpadding with the standard library.
func (a *fixture) decrypt(t *testing.T, data, iv []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(a.key)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		t.Fatalf("encrypted length %d", len(data))
	}

	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)

	pad := int(out[len(out)-1])
	switch {
	case pad < 1, pad > aes.BlockSize:
		t.Fatalf("bad padding %d", pad)
	case !bytes.Equal(out[len(out)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)):
		t.Fatalf("bad padding bytes %d", pad)
	}
	return out[:len(out)-pad]
}

// hls returns encryption with the test key
func (a *fixture) hls(t *testing.T) *encrypt.HLS {
	t.Helper()

	h, err := encrypt.New(a.key)
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func TestKeySize(t *testing.T) {
	f := fixture{}
	f.init()

	_, iv, err := f.hls(t).Segment([]byte("x"))
	if err != nil {
		t.Fatalf("16 byte key: %v", err)
	}
	if len(iv) != 16 {
		t.Fatalf("16 byte key: iv %d", len(iv))
	}

	for _, n := range []int{0, 15, 17, 32} {
		h, err := encrypt.New(make([]byte, n))
		switch {
		case err == nil:
			t.Errorf("%d byte key accepted", n)
		case h != nil:
			t.Errorf("%d byte key: cipher returned", n)
		}
	}
}

func TestPadding(t *testing.T) {
	f := fixture{}
	f.init()

	for _, n := range []int{0, 1, 15, 16, 17, 31, 32} {
		enc, _, err := f.hls(t).Segment(make([]byte, n))
		if err != nil {
			t.Fatal(err)
		}
		want := (n/aes.BlockSize + 1) * aes.BlockSize
		if len(enc) != want {
			t.Errorf("%d bytes: encrypted %d, want %d", n, len(enc), want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	f := fixture{}
	f.init()

	for _, n := range []int{0, 1, 15, 16, 17, 4096, 1 << 20} {
		data := make([]byte, n)
		rand.Read(data)
		enc, iv, err := f.hls(t).Segment(data)
		if err != nil {
			t.Fatal(err)
		}
		if n >= aes.BlockSize && bytes.Contains(enc, data[:aes.BlockSize]) {
			t.Errorf("%d bytes: plaintext visible", n)
		}
		got := f.decrypt(t, enc, iv)
		if !bytes.Equal(got, data) {
			t.Errorf("%d bytes: round trip differs", n)
		}
	}
}

func TestUniqueIV(t *testing.T) {
	f := fixture{}
	f.init()

	data := []byte("same segment")
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		_, iv, err := f.hls(t).Segment(data)
		if err != nil {
			t.Fatal(err)
		}
		if seen[string(iv)] {
			t.Fatalf("iv repeated after %d calls", i)
		}
		seen[string(iv)] = true
	}
}

func TestOpenSSL(t *testing.T) {
	bin, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl is not installed")
	}
	f := fixture{}
	f.init()

	dir := t.TempDir()
	for _, n := range []int{1, 16, 4097, 100000} {
		data := make([]byte, n)
		rand.Read(data)
		enc, iv, err := f.hls(t).Segment(data)
		if err != nil {
			t.Fatal(err)
		}

		in := filepath.Join(dir, "encrypted.m4s")
		out := filepath.Join(dir, "decrypted.m4s")
		err = os.WriteFile(in, enc, 0o644)
		if err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command(bin, "enc", "-d", "-aes-128-cbc", "-K", hex.EncodeToString(f.key), "-iv", hex.EncodeToString(iv), "-nosalt", "-in", in, "-out", out)
		msg, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%d bytes: openssl: %v %s", n, err, msg)
		}

		got, err := os.ReadFile(out)
		switch {
		case err != nil:
			t.Errorf("%d bytes: %v", n, err)
		case !bytes.Equal(got, data):
			t.Errorf("%d bytes: openssl output differs", n)
		}
	}
}

func TestPlaylist(t *testing.T) {
	f := fixture{}
	f.init()

	// one iv per segment
	ivs := map[string][]byte{}
	for _, name := range []string{"seg_0000.m4s", "seg_0001.m4s", "seg_0002.m4s"} {
		_, iv, err := f.hls(t).Segment([]byte(name))
		if err != nil {
			t.Fatal(err)
		}
		ivs[name] = iv
	}

	text, err := f.hls(t).Playlist(f.playlist, "bee://1001", ivs)
	if err != nil {
		t.Fatal(err)
	}

	// every key line sits above the #EXTINF of its own segment; every
	// other line is unchanged and in order
	lines := strings.Split(text, "\n")
	var rest []string
	keys, mapAt, firstKey := 0, -1, -1
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			m := f.keyLine.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("bad key line %q", line)
			}
			switch {
			case i+2 >= len(lines):
				t.Errorf("key line %d is last:\n%s", i, text)
			case !strings.HasPrefix(lines[i+1], "#EXTINF:"):
				t.Errorf("key line %d is not above #EXTINF:\n%s", i, text)
			case m[1] != hex.EncodeToString(ivs[lines[i+2]]):
				t.Errorf("key line %d does not match its segment:\n%s", i, text)
			}
			if firstKey < 0 {
				firstKey = i
			}
			keys++
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			mapAt = i
			rest = append(rest, line)
		default:
			rest = append(rest, line)
		}
	}
	if keys != 3 {
		t.Errorf("%d key lines, want 3", keys)
	}
	if mapAt < 0 || mapAt > firstKey {
		t.Errorf("EXT-X-MAP must stay above the first key:\n%s", text)
	}
	if strings.Join(rest, "\n") != f.playlist {
		t.Errorf("other lines changed:\n%s", text)
	}
	if strings.Contains(text, hex.EncodeToString(f.key)) || strings.Contains(text, string(f.key)) {
		t.Error("playlist contains the key")
	}

	// a missing or short iv fails
	delete(ivs, "seg_0001.m4s")
	_, err = f.hls(t).Playlist(f.playlist, "bee://1001", ivs)
	if err == nil {
		t.Error("missing iv must fail")
	}
	ivs["seg_0001.m4s"] = make([]byte, 8)
	_, err = f.hls(t).Playlist(f.playlist, "bee://1001", ivs)
	if err == nil {
		t.Error("short iv must fail")
	}
}

// TestPlaylistURI — an empty uri, or one that would break out of the quoted
// attribute or the line, is refused.
func TestPlaylistURI(t *testing.T) {
	f := fixture{}
	f.init()

	ivs := map[string][]byte{}
	for _, uri := range []string{"", `bee://1"`, "bee://1\n#EXT-X-ENDLIST", "bee://1\r"} {
		_, err := f.hls(t).Playlist(f.playlist, uri, ivs)
		if err == nil {
			t.Errorf("uri %q accepted", uri)
		}
	}
}

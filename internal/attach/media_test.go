package attach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenMedia tells pictures, videos (MP4, MOV) and audio (MP3, WAV) apart by
// their bytes, and refuses anything else.
func TestOpenMediaFamilies(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		data   []byte
		family string
		ct     string
	}{
		"a.png": {pngBytes(4, 4), "image", "image/png"},
		"a.mp4": {append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0}, make([]byte, 32)...), "video", "video/mp4"},
		"a.mov": {append([]byte{0, 0, 0, 0x14, 'f', 't', 'y', 'p', 'q', 't', ' ', ' ', 0, 0, 2, 0}, make([]byte, 32)...), "video", "video/quicktime"},
		"a.mp3": {append([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"), make([]byte, 32)...), "audio", "audio/mpeg"},
		"a.wav": {append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 32)...), "audio", "audio/wave"},
	}
	for name, c := range cases {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, c.data, 0o644)
		m, err := OpenMedia(p)
		if err != nil || m.Family != c.family || m.ContentType != c.ct {
			t.Errorf("%s: %+v %v", name, m, err)
		}
	}
	p := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o644)
	if _, err := OpenMedia(p); err == nil || !strings.Contains(err.Error(), "MP4, MOV") {
		t.Fatalf("text: %v", err)
	}
}

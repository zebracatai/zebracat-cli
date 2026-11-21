// Package attach turns pictures the person gives the CLI — pasted from the
// clipboard, dragged in as a file path, or passed with --image — into checked
// images, uploads them to the public API (POST /api/v1/public/uploads, one
// multipart "file"), and hands the plan the stored URLs as its uploads.
package attach

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg" // DecodeConfig
	_ "image/png"  // DecodeConfig
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

// MaxBytes is the studio's photo cap (squanchy FaceVideoPanel MAX_PHOTO_BYTES).
const MaxBytes = 12 << 20

// UploadRoute is the public API's picture upload (PNG/JPEG/WebP ≤ 12 MB).
const UploadRoute = "/api/v1/public/uploads"

// Kinds the plan takes for an upload (backend doors.UPLOAD_KINDS); "other"
// lets the plan read the picture itself.
var Kinds = []string{"person", "place", "product", "style", "logo", "other"}

// formats the studio accepts, by sniffed content type.
var formats = map[string]string{"image/png": "png", "image/jpeg": "jpeg", "image/webp": "webp"}

// Image is one checked picture.
type Image struct {
	Path   string
	Format string // png | jpeg | webp
	Width  int
	Height int
	Bytes  int64
	Kind   string // for the plan: person | place | product | style | logo | other
	Temp   bool   // a clipboard image saved to a temp file (removed after sending)
}

// Chip is how the image shows in the input: "[Image #1 · 1080×1920 · 240 KB]".
func (im Image) Chip(n int) string {
	dims := ""
	if im.Width > 0 && im.Height > 0 {
		dims = fmt.Sprintf(" · %d×%d", im.Width, im.Height)
	}
	return fmt.Sprintf("[Image #%d%s · %s]", n, dims, Size(im.Bytes))
}

// Size is a byte count as "240 KB" / "1.4 MB".
func Size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", (n+512)>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// Open checks a file is a picture the studio takes and reads its size.
func Open(path string) (Image, error) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return Image{}, fmt.Errorf("%s: no such file", filepath.Base(path))
	}
	if st.Size() > MaxBytes {
		return Image{}, fmt.Errorf("%s is %s; pictures can be up to %s", filepath.Base(path), Size(st.Size()), Size(MaxBytes))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Image{}, fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	format := formats[http.DetectContentType(b)]
	if format == "" {
		return Image{}, fmt.Errorf("%s isn't a PNG, JPEG or WebP picture", filepath.Base(path))
	}
	im := Image{Path: path, Format: format, Bytes: st.Size(), Kind: "other"}
	if format == "webp" {
		im.Width, im.Height = webpSize(b)
	} else if cfg, _, err := image.DecodeConfig(bytes.NewReader(b)); err == nil {
		im.Width, im.Height = cfg.Width, cfg.Height
	}
	return im, nil
}

// webpSize reads a WebP's canvas size from its header (VP8X, VP8 or VP8L).
func webpSize(b []byte) (int, int) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0
	}
	switch string(b[12:16]) {
	case "VP8X":
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1
	case "VP8 ":
		return int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff)
	case "VP8L":
		bits := binary.LittleEndian.Uint32(b[21:25])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1
	}
	return 0, 0
}

// ---- pasted text that names image files ----------------------------------------

// words splits pasted text the way a shell would read dropped paths: spaces
// separate words; '…' and "…" quote; a backslash escapes the next character
// (macOS terminals drop "my\ photo.png").
func words(s string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	escaped, any := false, false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, any = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, any = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if cur.Len() > 0 || any {
				out = append(out, cur.String())
			}
			cur.Reset()
			any = false
		default:
			cur.WriteRune(r)
			any = true
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// localPath reads one word as a local file path: an absolute path, ~/…, or a
// file:// URL; "" otherwise.
func localPath(w string) string {
	if strings.HasPrefix(w, "file://") {
		u, err := url.Parse(w)
		if err != nil || (u.Host != "" && u.Host != "localhost") {
			return ""
		}
		return u.Path
	}
	if strings.HasPrefix(w, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, w[2:])
		}
	}
	if filepath.IsAbs(w) || (len(w) > 2 && w[1] == ':' && (w[2] == '\\' || w[2] == '/')) {
		return w
	}
	return ""
}

// Paths reads pasted text as one or more dropped image files. It returns the
// images only when EVERY word is an existing picture; otherwise the paste is
// text (a sentence that mentions a path, or a non-image file, stays text).
// A word that names a picture the studio can't take returns that error.
func Paths(text string) ([]Image, error) {
	ws := words(strings.TrimSpace(text))
	if len(ws) == 0 {
		return nil, nil
	}
	var out []Image
	for _, w := range ws {
		p := localPath(w)
		if p == "" {
			return nil, nil
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return nil, nil
		}
		im, err := Open(p)
		if err != nil {
			if imageExt(p) {
				return nil, err // it is meant as a picture: say why it can't be used
			}
			return nil, nil // another file: keep the text
		}
		out = append(out, im)
	}
	return out, nil
}

func imageExt(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".heic", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

// ---- upload --------------------------------------------------------------------

// Stored is one uploaded picture: its URL, and what the server thinks it shows
// (kind_hint: person | place | product | style | logo | other, or "").
type Stored struct {
	URL      string `json:"url"`
	KindHint string `json:"kind_hint"`
}

// Upload sends one picture to POST /api/v1/public/uploads (multipart, one
// "file") and returns where it was stored.
func Upload(ctx context.Context, cl *client.Client, im Image) (Stored, error) {
	data, err := os.ReadFile(im.Path)
	if err != nil {
		return Stored{}, clierr.Usage("cannot read %s: %v", im.Path, err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	name := filepath.Base(im.Path)
	if im.Temp {
		ext := im.Format
		if ext == "jpeg" {
			ext = "jpg"
		}
		name = "pasted-image." + ext
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	h.Set("Content-Type", "image/"+im.Format)
	part, err := mw.CreatePart(h)
	if err != nil {
		return Stored{}, clierr.API("%v", err)
	}
	_, _ = part.Write(data)
	_ = mw.Close()
	var out Stored
	if _, err := cl.Do(ctx, "POST", UploadRoute, client.RawBody{ContentType: mw.FormDataContentType(), Data: body.Bytes()}, &out); err != nil {
		return Stored{}, err
	}
	if out.URL == "" {
		return Stored{}, clierr.API("upload of %s failed: the server answered no url", name)
	}
	return out, nil
}

// Uploads is the plan request's uploads field for stored pictures. The kind is
// the one the person gave, else the server's kind_hint, else "other".
func Uploads(stored []Stored, imgs []Image) []map[string]any {
	out := make([]map[string]any, 0, len(stored))
	for i, st := range stored {
		kind := "other"
		if i < len(imgs) && imgs[i].Kind != "" {
			kind = imgs[i].Kind
		} else if knownKind(st.KindHint) {
			kind = st.KindHint
		}
		out = append(out, map[string]any{"url": st.URL, "kind": kind})
	}
	return out
}

func knownKind(k string) bool {
	for _, v := range Kinds {
		if v == k {
			return true
		}
	}
	return false
}

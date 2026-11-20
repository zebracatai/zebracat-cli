package attach

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

// LinkRoute is the public API's one-time upload link (api-gap-audit G18/G19):
// it answers where to send one file and the URL the file will have. Video and
// audio files go this way; the size and length caps are the server's.
const LinkRoute = "/api/v1/public/uploads/link"

// Media is one local file to upload: a picture, a video or an audio file.
type Media struct {
	Path        string
	ContentType string // image/png, video/mp4, audio/mpeg, …
	Family      string // image | video | audio
	Bytes       int64
}

// videoAudio are the non-picture types the upload link takes (O6: MP4, MOV,
// MP3, WAV), by sniffed or extension content type.
var videoAudio = map[string]string{
	"video/mp4":       "video",
	"video/quicktime": "video",
	"audio/mpeg":      "audio",
	"audio/wav":       "audio",
	"audio/wave":      "audio",
	"audio/x-wav":     "audio",
}

// OpenMedia checks a local file is a picture (PNG, JPEG, WebP ≤ 12 MB), a
// video (MP4, MOV) or an audio file (MP3, WAV), by its first bytes.
func OpenMedia(path string) (Media, error) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return Media{}, fmt.Errorf("%s: no such file", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return Media{}, fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	f.Close()
	head = head[:n]
	ct := sniffMedia(head, path)
	if format := formats[ct]; format != "" {
		if st.Size() > MaxBytes {
			return Media{}, fmt.Errorf("%s is %s; pictures can be up to %s", filepath.Base(path), Size(st.Size()), Size(MaxBytes))
		}
		return Media{Path: path, ContentType: ct, Family: "image", Bytes: st.Size()}, nil
	}
	if fam := videoAudio[ct]; fam != "" {
		return Media{Path: path, ContentType: ct, Family: fam, Bytes: st.Size()}, nil
	}
	return Media{}, fmt.Errorf("%s isn't a picture (PNG, JPEG, WebP), a video (MP4, MOV) or an audio file (MP3, WAV)", filepath.Base(path))
}

// sniffMedia names the content type from the file's first bytes; a QuickTime
// movie (which Go's sniffer doesn't know) is read from its ftyp box.
func sniffMedia(head []byte, path string) string {
	if len(head) >= 12 && string(head[4:8]) == "ftyp" && string(head[8:10]) == "qt" {
		return "video/quicktime"
	}
	ct := http.DetectContentType(head)
	if i := strings.Index(ct, ";"); i > 0 {
		ct = ct[:i]
	}
	if ct == "application/octet-stream" {
		// An MP3 without an ID3 tag starts with a frame sync; trust the
		// extension only for the types the upload takes.
		if byExt, _, _ := mime.ParseMediaType(mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))); videoAudio[byExt] != "" {
			return byExt
		}
	}
	return ct
}

// UploadMedia sends one local file to Zebracat storage and returns its URL:
// a picture through POST /uploads (multipart file, kind, name), a video or an
// audio file through a one-time upload link. kind and name may be "".
func UploadMedia(ctx context.Context, cl *client.Client, m Media, kind, name string) (Stored, error) {
	if m.Family == "image" {
		return uploadPicture(ctx, cl, m, kind, name)
	}
	return uploadByLink(ctx, cl, m, kind, name)
}

func uploadPicture(ctx context.Context, cl *client.Client, m Media, kind, name string) (Stored, error) {
	data, err := os.ReadFile(m.Path)
	if err != nil {
		return Stored{}, clierr.Usage("cannot read %s: %v", m.Path, err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filepath.Base(m.Path)))
	h.Set("Content-Type", m.ContentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return Stored{}, clierr.API("%v", err)
	}
	_, _ = part.Write(data)
	if kind != "" {
		_ = mw.WriteField("kind", kind)
	}
	if name != "" {
		_ = mw.WriteField("name", name)
	}
	_ = mw.Close()
	var out Stored
	if _, err := cl.Do(ctx, "POST", UploadRoute, client.RawBody{ContentType: mw.FormDataContentType(), Data: body.Bytes()}, &out); err != nil {
		return Stored{}, err
	}
	if out.URL == "" {
		return Stored{}, clierr.API("upload of %s failed: the server answered no url", filepath.Base(m.Path))
	}
	return out, nil
}

// link is the upload link's answer: where to send the file, how, and the URL
// the file has once it is there.
type link struct {
	UploadURL string            `json:"upload_url"`
	URL       string            `json:"url"`
	Method    string            `json:"method"`  // PUT (the body is the file) or POST (multipart "file"); default POST
	Headers   map[string]string `json:"headers"` // headers the upload must carry
	Fields    map[string]string `json:"fields"`  // form fields a POST upload must carry
}

// Transfer is the HTTP client for the upload link itself. It never carries the
// API key: the link authorizes the one file. Tests swap it.
var Transfer = &http.Client{}

func uploadByLink(ctx context.Context, cl *client.Client, m Media, kind, name string) (Stored, error) {
	req := map[string]any{"filename": filepath.Base(m.Path), "content_type": m.ContentType, "size": m.Bytes}
	if kind != "" {
		req["kind"] = kind
	}
	if name != "" {
		req["name"] = name
	}
	var l link
	if _, err := cl.Do(ctx, "POST", LinkRoute, req, &l); err != nil {
		return Stored{}, err
	}
	if l.UploadURL == "" {
		return Stored{}, clierr.API("the upload link answered no upload_url")
	}
	f, err := os.Open(m.Path)
	if err != nil {
		return Stored{}, clierr.Usage("cannot read %s: %v", m.Path, err)
	}
	defer f.Close()

	var hreq *http.Request
	if strings.EqualFold(l.Method, "PUT") {
		hreq, err = http.NewRequestWithContext(ctx, "PUT", l.UploadURL, f)
		if err == nil {
			hreq.ContentLength = m.Bytes
			hreq.Header.Set("Content-Type", m.ContentType)
		}
	} else {
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		go func() {
			for k, v := range l.Fields {
				_ = mw.WriteField(k, v)
			}
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filepath.Base(m.Path)))
			h.Set("Content-Type", m.ContentType)
			part, err := mw.CreatePart(h)
			if err == nil {
				_, err = io.Copy(part, f)
			}
			if err == nil {
				err = mw.Close()
			}
			pw.CloseWithError(err)
		}()
		hreq, err = http.NewRequestWithContext(ctx, "POST", l.UploadURL, pr)
		if err == nil {
			hreq.Header.Set("Content-Type", mw.FormDataContentType())
		}
	}
	if err != nil {
		return Stored{}, clierr.API("bad upload link: %v", err)
	}
	for k, v := range l.Headers {
		hreq.Header.Set(k, v)
	}
	hreq.Header.Set("User-Agent", client.UserAgent())
	resp, err := Transfer.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return Stored{}, clierr.Timeout("the upload of %s timed out", filepath.Base(m.Path))
		}
		return Stored{}, clierr.Network("the upload of %s broke: %v", filepath.Base(m.Path), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		e := clierr.API("the upload of %s failed (HTTP %d): %s", filepath.Base(m.Path), resp.StatusCode, strings.TrimSpace(string(raw)))
		e.Status = resp.StatusCode
		return Stored{}, e
	}
	out := Stored{URL: l.URL}
	var answer Stored
	if b := bytes.TrimSpace(raw); len(b) > 0 && b[0] == '{' {
		_ = json.Unmarshal(b, &answer)
	}
	if answer.URL != "" {
		out.URL = answer.URL
	}
	if answer.KindHint != "" {
		out.KindHint = answer.KindHint
	}
	if out.URL == "" {
		return Stored{}, clierr.API("the upload of %s answered no url", filepath.Base(m.Path))
	}
	return out, nil
}

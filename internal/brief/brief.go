// Package brief turns what a person typed into the Director v2 create body the
// studio's /chat sends: the words as the brief, a linked video post as the run's
// source_video (the plan model decides whether to clone it or use it as the
// topic), a lone web page link as the URL door, and always a plan only
// (auto_generate false): Generate is a separate, confirmed step.
//
// Links are read from the URL's own parts (net/url), never by pattern: the same
// rules as the studio's videoPostLink.
package brief

import (
	"net/url"
	"path"
	"strings"
)

// wrappers are characters a link in a sentence is wrapped in or followed by.
const wrappers = ".,;:!?()[]{}<>\"'`“”‘’«»"

// videoExts are direct video files a person may paste (a public MP4 link).
var videoExts = map[string]bool{".mp4": true, ".mov": true, ".webm": true, ".m4v": true}

func present(s string) bool { return s != "" }

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func at(s []string, i int) string {
	if i >= 0 && i < len(s) {
		return s[i]
	}
	return ""
}

func oneOf(v string, set ...string) bool { return indexOf(set, v) >= 0 }

type postPath func(seg []string, u *url.URL, host string) bool

var youtube postPath = func(s []string, u *url.URL, _ string) bool {
	return (at(s, 0) == "watch" && present(u.Query().Get("v"))) ||
		(oneOf(at(s, 0), "shorts", "live", "embed", "v") && present(at(s, 1)))
}

func single(s []string, _ *url.URL, _ string) bool { return len(s) == 1 }

// postPaths: per platform, is this path one video post (mirrors squanchy's
// src/services/aiChat/videoPostLink.ts).
var postPaths = map[string]postPath{
	"tiktok.com": func(s []string, _ *url.URL, _ string) bool {
		i := indexOf(s, "video")
		return (i > 0 && present(at(s, i+1))) || (oneOf(at(s, 0), "t", "v") && present(at(s, 1)))
	},
	"vm.tiktok.com": single,
	"vt.tiktok.com": single,
	"instagram.com": func(s []string, _ *url.URL, _ string) bool {
		return oneOf(at(s, 0), "reel", "reels", "tv") && present(at(s, 1))
	},
	"instagr.am": func(s []string, _ *url.URL, _ string) bool {
		return oneOf(at(s, 0), "reel", "reels", "tv") && present(at(s, 1))
	},
	"youtube.com":   youtube,
	"m.youtube.com": youtube,
	"youtu.be":      single,
	"facebook.com": func(s []string, u *url.URL, _ string) bool {
		i := indexOf(s, "videos")
		return (at(s, 0) == "watch" && present(u.Query().Get("v"))) ||
			(at(s, 0) == "reel" && present(at(s, 1))) ||
			(at(s, 0) == "share" && oneOf(at(s, 1), "v", "r") && present(at(s, 2))) ||
			(i >= 0 && present(at(s, i+1)))
	},
	"fb.watch":  single,
	"vimeo.com": func(s []string, _ *url.URL, _ string) bool { return digits(at(s, len(s)-1)) },
	"snapchat.com": func(s []string, _ *url.URL, _ string) bool {
		return at(s, 0) == "spotlight" && present(at(s, 1))
	},
	"twitch.tv": func(s []string, _ *url.URL, host string) bool {
		return (host == "clips.twitch.tv" && len(s) == 1) ||
			(at(s, 0) == "videos" && present(at(s, 1))) ||
			(at(s, 1) == "clip" && present(at(s, 2)))
	},
	"dailymotion.com": func(s []string, _ *url.URL, _ string) bool { return at(s, 0) == "video" && present(at(s, 1)) },
	"dai.ly":          single,
	"bilibili.com":    func(s []string, _ *url.URL, _ string) bool { return at(s, 0) == "video" && present(at(s, 1)) },
	"b23.tv":          single,
}

// words splits text on whitespace and trims the punctuation around each word.
func words(text string) []string {
	var out []string
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, wrappers)
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// webURL parses one word as an http(s) URL with a host, or nil.
func webURL(word string) *url.URL {
	u, err := url.Parse(word)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	return u
}

func platformOf(host string) string {
	if _, ok := postPaths[host]; ok {
		return host
	}
	for known := range postPaths {
		if strings.HasSuffix(host, "."+known) {
			return known
		}
	}
	return ""
}

// VideoLink reports whether one word is a link to ONE video: a post on a known
// platform, or a direct video file.
func VideoLink(word string) (string, bool) {
	u := webURL(word)
	if u == nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if videoExts[strings.ToLower(path.Ext(u.Path))] {
		return u.String(), true
	}
	p := platformOf(host)
	if p == "" {
		return "", false
	}
	var seg []string
	for _, s := range strings.Split(u.Path, "/") {
		if s != "" {
			seg = append(seg, s)
		}
	}
	if postPaths[p](seg, u, host) {
		return u.String(), true
	}
	return "", false
}

// Kind is how a typed message is sent.
type Kind string

const (
	KindIdea   Kind = "idea"       // plain words
	KindVideo  Kind = "video_link" // words (or nothing else) with a video post: source_video
	KindPage   Kind = "web_page"   // a lone link to a page: the URL door
	KindScript Kind = "script"     // a script file the person loaded on purpose
)

// Read classifies a message and returns the link it carries, if any.
func Read(text string) (Kind, string) {
	ws := words(text)
	for _, w := range ws {
		if link, ok := VideoLink(w); ok {
			return KindVideo, link
		}
	}
	if len(ws) == 1 {
		if u := webURL(ws[0]); u != nil {
			return KindPage, u.String()
		}
	}
	return KindIdea, ""
}

// Body is the POST /videos body for a message: a plan only.
func Body(text string, kind Kind, shouldRender bool) map[string]any {
	text = strings.TrimSpace(text)
	body := map[string]any{"brief": text, "input_kind": "idea", "auto_generate": false, "should_render": shouldRender}
	switch kind {
	case KindScript:
		body["input_kind"] = "script"
	default:
		switch k, link := Read(text); k {
		case KindVideo:
			body["source_video"] = link
		case KindPage:
			body["brief"] = link
			body["input_kind"] = "url"
		}
	}
	return body
}

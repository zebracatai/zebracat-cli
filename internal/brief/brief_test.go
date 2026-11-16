package brief

import "testing"

func TestVideoLink(t *testing.T) {
	yes := []string{
		"https://www.tiktok.com/@someone/video/7300000000000000000",
		"https://vm.tiktok.com/ZMabc123/",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/shorts/abc123",
		"https://youtu.be/abc123",
		"https://www.instagram.com/reel/Cx1/",
		"https://www.facebook.com/watch?v=123",
		"https://vimeo.com/123456",
		"https://cdn.example.com/clips/me.mp4",
	}
	no := []string{
		"https://www.tiktok.com/@someone",       // a profile
		"https://www.youtube.com/@channel",      // a channel
		"https://www.instagram.com/p/Cx1/",      // may be a photo
		"https://example.com/blog/how-we-roast", // a page
		"ftp://example.com/video.mp4",           // not the web
		"tiktok.com/@someone/video/1",           // no scheme
		"hello",
	}
	for _, s := range yes {
		if _, ok := VideoLink(s); !ok {
			t.Errorf("%s should be a video link", s)
		}
	}
	for _, s := range no {
		if _, ok := VideoLink(s); ok {
			t.Errorf("%s should not be a video link", s)
		}
	}
}

func TestBody(t *testing.T) {
	b := Body("a 30 second ad for my coffee brand", KindIdea, true)
	if b["input_kind"] != "idea" || b["auto_generate"] != false || b["should_render"] != true || b["source_video"] != nil {
		t.Fatalf("idea: %v", b)
	}
	b = Body("clone this (https://youtu.be/abc123).", KindIdea, true)
	if b["source_video"] != "https://youtu.be/abc123" || b["brief"] != "clone this (https://youtu.be/abc123)." || b["input_kind"] != "idea" {
		t.Fatalf("video link: %v", b)
	}
	b = Body("  https://example.com/post  ", KindIdea, false)
	if b["input_kind"] != "url" || b["brief"] != "https://example.com/post" || b["should_render"] != false {
		t.Fatalf("page: %v", b)
	}
	b = Body("read about https://example.com/post and make it fun", KindIdea, true)
	if b["input_kind"] != "idea" || b["source_video"] != nil {
		t.Fatalf("a page link inside words stays an idea: %v", b)
	}
	b = Body("NARRATOR: Hello https://youtu.be/abc123", KindScript, true)
	if b["input_kind"] != "script" || b["source_video"] != nil {
		t.Fatalf("a script is a script: %v", b)
	}
}

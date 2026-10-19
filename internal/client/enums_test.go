package client

import (
	"os"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/openapi"
)

// The committed enums_gen.go must be what `go generate` makes from the
// committed schema copy: a schema refresh without a regenerate fails CI.
func TestEnumsGenMatchesSchema(t *testing.T) {
	doc, err := openapi.Load()
	if err != nil {
		t.Fatal(err)
	}
	want, err := openapi.GoSource(doc.Enums())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("enums_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("internal/client/enums_gen.go is stale: run `go generate ./...` (or `make openapi`)")
	}
}

// The engine's vocabularies (PLAN §1.10) reach the CLI through the schema.
func TestEnumsCarryTheEngineVocabularies(t *testing.T) {
	cases := map[string][]string{
		"create.input_kind":         {"idea", "script", "voiceover_script", "script_with_scenes", "url"},
		"create.options.media_type": {"ai_video", "ai_image_video", "brainrot", "stock"},
		"create.options.audio_mode": {"narration", "dialogue", "silent"},
		"create.options.aspect":     {"vertical", "horizontal", "square"},
		"create.options.quality":    {"quick", "balanced", "cinematic", "auto"},
		"create.uploads.kind":       {"person", "place", "product", "style", "other"},
		"clone.mode":                {"generate_similar", "recreate_script", "remix", "remake_niche", "avatar_reaction", "face_swap"},
	}
	for key, want := range cases {
		got := Enum(key)
		if len(got) != len(want) {
			t.Fatalf("%s = %v, want %v", key, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s = %v, want %v", key, got, want)
			}
		}
	}
	for _, key := range []string{"create.preset", "plan_edit.tool", "video.phase"} {
		if len(Enum(key)) == 0 {
			t.Fatalf("%s has no values", key)
		}
	}
	if !CheckEnum("create.options.media_type", "stock") || CheckEnum("create.options.media_type", "avatar") {
		t.Fatal("CheckEnum disagrees with the schema")
	}
	if !CheckEnum("no.such.key", "anything") {
		t.Fatal("an undeclared key must allow anything")
	}
}

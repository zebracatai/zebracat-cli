package cmd

import "strings"

// scrubControl removes control characters (ESC, C0/C1, DEL; newlines and tabs
// stay) from every string in decoded JSON, in place, before it is rendered
// for a person: text from the server or from other people's videos
// (Inspiration) can never move the cursor, retitle the window or hide lines.
// JSON output needs no scrub: the encoder escapes them.
func scrubControl(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if s, ok := x.(string); ok {
				t[k] = cleanText(s)
			} else {
				scrubControl(x)
			}
		}
	case []any:
		for i, x := range t {
			if s, ok := x.(string); ok {
				t[i] = cleanText(s)
			} else {
				scrubControl(x)
			}
		}
	case []map[string]any:
		for _, x := range t {
			scrubControl(x)
		}
	}
}

func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

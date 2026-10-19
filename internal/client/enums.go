package client

import (
	"sort"
	"strings"
)

//go:generate go run ../openapi/enumgen -out enums_gen.go

// Enum returns the allowed values for key (see Enums), or nil when the schema
// does not declare them — callers then leave the check to the server.
func Enum(key string) []string { return Enums[key] }

// CheckEnum reports whether v is allowed for key. An unknown key allows anything.
func CheckEnum(key, v string) bool {
	vals := Enums[key]
	if len(vals) == 0 {
		return true
	}
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}

// EnumHelp renders the allowed values as "a|b|c" for flag help text.
func EnumHelp(key string) string {
	vals := append([]string(nil), Enums[key]...)
	if len(vals) > 12 {
		sort.Strings(vals)
	}
	return strings.Join(vals, "|")
}

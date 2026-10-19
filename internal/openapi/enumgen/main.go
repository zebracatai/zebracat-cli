// Command enumgen writes internal/client/enums_gen.go from the checked-in
// OpenAPI copy. It runs from `go generate ./...` (see internal/client/enums.go).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/zebracatai/zebracat-cli/internal/openapi"
)

func main() {
	out := flag.String("out", "enums_gen.go", "output file")
	validate := flag.String("validate", "", "only check that this file is an OpenAPI 3 JSON schema with POST /videos")
	flag.Parse()
	if *validate != "" {
		raw, err := os.ReadFile(*validate)
		if err == nil {
			var doc *openapi.Doc
			if doc, err = openapi.Parse(raw); err == nil {
				if _, op := doc.Find("POST", "/videos"); op == nil {
					err = fmt.Errorf("%s has no POST /videos: the server does not serve the v2 contract yet", *validate)
				}
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	doc, err := openapi.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	src, err := openapi.GoSource(doc.Enums())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

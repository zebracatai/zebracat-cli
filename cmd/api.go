package cmd

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

var (
	apiData  string
	apiQuery []string
	apiYes   bool
)

var apiCmd = &cobra.Command{
	Use:   "api <METHOD> <path> [-d json]",
	Short: "Call any public API route with your key (raw JSON in and out)",
	Long: `Call any route of the public API with your key and print its JSON answer:
the way to use a route the day it ships, before it has a command.

<path> is relative to /api/v1/public (a path starting with /api is used as is).
-d takes a JSON body, @file to read one, or - for stdin. --query adds key=value.

A raw call can spend or delete, so anything but GET asks first on a terminal;
scripts and agents pass --yes.`,
	Example: `  zebracat api GET /video/models
  zebracat api GET /voices --query language=english --query gender=female
  zebracat api POST /videos -d '{"brief":"a cat astronaut","dry_run":true}' --yes`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		method := strings.ToUpper(args[0])
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return clierr.Usage("METHOD must be GET, POST, PUT, PATCH or DELETE, got %q", args[0])
		}
		path := args[1]
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if !strings.HasPrefix(path, "/api/") {
			path = pub + path
		}
		if len(apiQuery) > 0 {
			q := url.Values{}
			for _, kv := range apiQuery {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return clierr.Usage("--query takes key=value, got %q", kv)
				}
				q.Add(k, v)
			}
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			path += sep + q.Encode()
		}
		var body any
		if apiData != "" {
			raw := apiData
			if raw == "-" || strings.HasPrefix(raw, "@") {
				s, err := readTextArg("-d", strings.TrimPrefix(raw, "@"))
				if err != nil {
					return err
				}
				raw = s
			}
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				return clierr.Usage("-d must be JSON: %v", err)
			}
		}
		if method != "GET" {
			ok, err := confirmSpend(apiYes, "a "+method+" call", func() (string, error) {
				return method + " " + path + " can change or spend. Send it?", nil
			})
			if err != nil {
				return err
			}
			if !ok {
				return emit(map[string]any{"sent": false}, nil)
			}
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(3 * time.Minute)
		defer cancel()
		var out any
		if _, err := c.Do(ctx, method, path, body, &out); err != nil {
			return err
		}
		if out == nil {
			out = map[string]any{}
		}
		return emit(out, nil)
	},
}

func init() {
	apiCmd.Flags().StringVarP(&apiData, "data", "d", "", "JSON body, @file, or - for stdin")
	apiCmd.Flags().StringArrayVar(&apiQuery, "query", nil, "A query parameter key=value (repeatable)")
	apiCmd.Flags().BoolVar(&apiYes, "yes", false, "Don't ask before a call that is not GET; required when not on a terminal")
	rootCmd.AddCommand(apiCmd)
}

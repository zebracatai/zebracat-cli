package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// clock and pollInterval drive every --wait; tests swap in a fake clock.
var (
	clock        videos.Clock = videos.RealClock
	pollInterval              = 5 * time.Second
)

// stdin is where file arguments of "-" and the Generate confirmation read from.
var stdin io.Reader = os.Stdin

// newIdempotencyKey is a random UUID v4: one per invocation unless the user
// passes --idempotency-key, so a retried command never spends twice.
func newIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// readTextArg reads a file path, or stdin for "-".
func readTextArg(flag, path string) (string, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return "", clierr.Usage("cannot read %s %q: %v", flag, path, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", clierr.Usage("%s %q is empty", flag, path)
	}
	return s, nil
}

// interactive reports whether we may ask the user a question: human output on
// a terminal with a terminal on stdin. JSON / piped runs never prompt.
var interactive = func() bool {
	return humanMode() && ui.IsTTY(os.Stdin) && ui.IsTTY(os.Stdout)
}

// confirm asks a yes/no question on stderr and reads the answer from stdin.
var confirm = func(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// note prints a one-line notice on stderr (deprecations, ignored flags); it
// never touches stdout, so JSON stays clean.
func note(format string, args ...any) {
	if flagQuiet {
		return
	}
	fmt.Fprintf(os.Stderr, "note: "+format+"\n", args...)
}

// postIdempotent POSTs with an Idempotency-Key. Under wait, a 429 is retried
// after its Retry-After (same key, so it can never create twice) until timeout.
func postIdempotent(c *client.Client, path string, body any, key string, wait bool, timeout time.Duration, out any) error {
	headers := map[string]string{"Idempotency-Key": key}
	deadline := clock.Now().Add(timeout)
	for {
		ctx, cancel := ctxTimeout(2 * time.Minute)
		_, err := c.DoWith(ctx, "POST", path, body, headers, out)
		cancel()
		var ce *clierr.Error
		if !wait || !errors.As(err, &ce) || ce.Status != 429 {
			return err
		}
		sleep := ce.RetryAfter
		if sleep <= 0 {
			sleep = 10 * time.Second
		}
		if !clock.Now().Add(sleep).Before(deadline) {
			return err
		}
		note("%s — retrying in %s", ce.Message, sleep)
		clock.Sleep(sleep)
	}
}

// getMap GETs a path into a map.
func getMap(c *client.Client, path string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var out map[string]any
	if _, err := c.Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

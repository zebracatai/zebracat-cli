//go:build windows

package update

import "errors"

// execFn: Windows has no exec; Relaunch refuses before calling it.
var execFn = func(string, []string, []string) error { return errors.New("not supported on Windows") }

//go:build !windows

package update

import "syscall"

// execFn replaces this process (a test swaps it).
var execFn = syscall.Exec

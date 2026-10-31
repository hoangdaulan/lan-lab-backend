//go:build !unix

package discovery

import "syscall"

// Without SO_REUSEADDR only one process per machine can bind the port.
func reuseAddr(_, _ string, _ syscall.RawConn) error { return nil }

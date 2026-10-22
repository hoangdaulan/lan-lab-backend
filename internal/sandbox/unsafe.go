package sandbox

import (
	"syscall"
	"unsafe"
)

// Pointer helpers for the raw syscalls in execMain.

func unsafePtr(b []byte) unsafe.Pointer             { return unsafe.Pointer(&b[0]) }
func unsafeByte(p *byte) unsafe.Pointer             { return unsafe.Pointer(p) }
func unsafeArgv(p **byte) unsafe.Pointer            { return unsafe.Pointer(p) }
func unsafeRlimit(r *syscall.Rlimit) unsafe.Pointer { return unsafe.Pointer(r) }

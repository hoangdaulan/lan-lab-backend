// Package sandbox runs an untrusted program with setrlimit limits, as an
// unprivileged uid, without network (BACKEND_CONTEXT.md §11, §19).
//
// How it works: the worker (root inside its container, with CAP_SYS_ADMIN)
// re-executes its own binary with ExecMarker. Go's fork/exec puts that child
// in a new network namespace (CLONE_NEWNET: only a down loopback) and
// switches it to the runner uid/gid with no supplementary groups, which
// drops every capability. The child (ExecMain) then sets no_new_privs and
// the rlimits using raw syscalls only, and execve()s the target, so the
// limits and identity apply to the program itself.
//
// Limits of this approach: memory is limited with RLIMIT_AS (virtual
// address space) and measured with ru_maxrss, not cgroups; RLIMIT_NPROC
// counts every process of the uid, so each concurrent slot uses its own
// uid; there is no filesystem namespace — the program sees the container
// filesystem with the permissions of the runner uid (DATA_DIR must not be
// readable by it).
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// ExecMarker / ReapMarker as os.Args[1] select the helper modes; the
	// binary that imports this package must call Main first thing.
	ExecMarker = "__sandbox_exec"
	ReapMarker = "__sandbox_reap"
)

// Limits are applied with setrlimit in the child (0 = leave unchanged,
// except Core which is always 0).
type Limits struct {
	CPUTime      time.Duration // RLIMIT_CPU (rounded up to seconds, +1 s hard)
	WallTime     time.Duration // killed by the parent after this (required)
	AddressSpace uint64        // RLIMIT_AS, bytes
	Stack        uint64        // RLIMIT_STACK, bytes
	FileSize     uint64        // RLIMIT_FSIZE, bytes (applies to stdout files)
	Processes    uint64        // RLIMIT_NPROC, per uid
	OpenFiles    uint64        // RLIMIT_NOFILE
}

type Spec struct {
	Path     string
	Args     []string
	Dir      string
	Env      []string
	Stdin    *os.File // nil = /dev/null
	Stdout   *os.File // nil = /dev/null
	Stderr   *os.File // nil = /dev/null
	UID, GID int
	Limits   Limits
}

type Result struct {
	ExitCode     int // -1 when killed by a signal
	Signaled     bool
	Signal       syscall.Signal
	CPUTime      time.Duration // user + system
	WallTime     time.Duration
	MaxRSSKB     int64
	WallTimedOut bool
}

// ErrSetup means the sandbox itself failed (an infrastructure error, not
// the program's fault).
var ErrSetup = errors.New("sandbox setup failed")

// rlimit resources (Linux numbering; NPROC is missing from package syscall).
const (
	rlimitCPU    = 0
	rlimitFSIZE  = 1
	rlimitSTACK  = 3
	rlimitCORE   = 4
	rlimitNPROC  = 6
	rlimitNOFILE = 7
	rlimitAS     = 9
)

const prSetNoNewPrivs = 38

func (l Limits) args() []string {
	cpu := uint64(0)
	if l.CPUTime > 0 {
		cpu = uint64((l.CPUTime + time.Second - 1) / time.Second)
	}
	return []string{
		strconv.FormatUint(cpu, 10),
		strconv.FormatUint(l.AddressSpace, 10),
		strconv.FormatUint(l.Stack, 10),
		strconv.FormatUint(l.FileSize, 10),
		strconv.FormatUint(l.Processes, 10),
		strconv.FormatUint(l.OpenFiles, 10),
	}
}

// Run executes spec and waits. The returned error is non-nil only for
// sandbox failures (wrapped ErrSetup) or context cancellation.
func Run(ctx context.Context, spec Spec) (Result, error) {
	if spec.Limits.WallTime <= 0 {
		return Result{}, fmt.Errorf("%w: wall time limit required", ErrSetup)
	}
	self, err := os.Executable()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrSetup, err)
	}
	statusR, statusW, err := os.Pipe()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrSetup, err)
	}
	defer statusR.Close()

	args := append([]string{ExecMarker}, spec.Limits.args()...)
	args = append(args, "--", spec.Path)
	args = append(args, spec.Args...)
	cmd := exec.Command(self, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	if cmd.Env == nil {
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = spec.Stdin, spec.Stdout, spec.Stderr
	if spec.Stdin == nil {
		cmd.Stdin = nil // os/exec opens /dev/null
	}
	cmd.ExtraFiles = []*os.File{statusW} // fd 3 in the child
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNET,
		Setpgid:    true,
		Pdeathsig:  syscall.SIGKILL,
		Credential: &syscall.Credential{Uid: uint32(spec.UID), Gid: uint32(spec.GID), Groups: []uint32{}},
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		statusW.Close()
		return Result{}, fmt.Errorf("%w: start: %v", ErrSetup, err)
	}
	statusW.Close()

	// The status pipe is close-on-exec in the child: EOF means execve
	// succeeded; any bytes are the helper's setup error.
	setupErr := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(statusR)
		setupErr <- string(b)
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(spec.Limits.WallTime)
	defer timer.Stop()
	var res Result
	var waitErr error
	select {
	case waitErr = <-done:
	case <-timer.C:
		res.WallTimedOut = true
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		waitErr = <-done
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return Result{}, ctx.Err()
	}
	res.WallTime = time.Since(start)
	if msg := <-setupErr; msg != "" {
		return Result{}, fmt.Errorf("%w: %s", ErrSetup, strings.TrimSpace(msg))
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return Result{}, fmt.Errorf("%w: wait: %v", ErrSetup, waitErr)
	}
	st := cmd.ProcessState
	ws := st.Sys().(syscall.WaitStatus)
	res.ExitCode = ws.ExitStatus()
	if ws.Signaled() {
		res.Signaled, res.Signal, res.ExitCode = true, ws.Signal(), -1
	}
	if ru, ok := st.SysUsage().(*syscall.Rusage); ok {
		res.CPUTime = time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
		res.MaxRSSKB = ru.Maxrss
	}
	return res, nil
}

// Reap kills every process left by uid (fork-and-exit, setsid escapes):
// kill(-1) from a helper running as that uid reaches exactly its processes.
func Reap(uid, gid int) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, ReapMarker)
	cmd.Env = []string{}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}},
	}
	return cmd.Run()
}

// Main dispatches the helper modes. Call it at the very start of main()
// (and TestMain); it returns normally when os.Args is not a helper call.
func Main() {
	if len(os.Args) < 2 {
		return
	}
	switch os.Args[1] {
	case ExecMarker:
		execMain(os.Args[2:])
	case ReapMarker:
		_ = syscall.Kill(-1, syscall.SIGKILL)
		os.Exit(0)
	}
}

// execMain: limits..., "--", path, args... Everything that allocates is
// done before the rlimits are set; after that only raw syscalls run, so a
// small RLIMIT_AS cannot break the Go runtime before execve.
func execMain(args []string) {
	// no_new_privs is per thread: prctl and execve must run on one thread.
	runtime.LockOSThread()
	fail := func(msg string) {
		_, _ = syscall.Write(3, []byte(msg))
		syscall.Exit(126)
	}
	if len(args) < 8 || args[6] != "--" {
		fail("bad helper arguments")
	}
	var vals [6]uint64
	for i := range vals {
		v, err := strconv.ParseUint(args[i], 10, 64)
		if err != nil {
			fail("bad limit " + args[i])
		}
		vals[i] = v
	}
	path := args[7]
	argv, err := syscall.SlicePtrFromStrings(append([]string{path}, args[8:]...))
	if err != nil {
		fail(err.Error())
	}
	envv, err := syscall.SlicePtrFromStrings(os.Environ())
	if err != nil {
		fail(err.Error())
	}
	pathp, err := syscall.BytePtrFromString(path)
	if err != nil {
		fail(err.Error())
	}
	cpu, as, stack, fsize, nproc, nofile := vals[0], vals[1], vals[2], vals[3], vals[4], vals[5]
	var lims [7]struct {
		res      int
		cur, max uint64
	}
	n := 0
	add := func(res int, cur, max uint64) {
		lims[n].res, lims[n].cur, lims[n].max = res, cur, max
		n++
	}
	add(rlimitCORE, 0, 0)
	if cpu > 0 {
		add(rlimitCPU, cpu, cpu+1)
	}
	if as > 0 {
		add(rlimitAS, as, as)
	}
	if stack > 0 {
		add(rlimitSTACK, stack, stack)
	}
	if fsize > 0 {
		add(rlimitFSIZE, fsize, fsize)
	}
	if nproc > 0 {
		add(rlimitNPROC, nproc, nproc)
	}
	if nofile > 0 {
		add(rlimitNOFILE, nofile, nofile)
	}
	errMsg := []byte("setrlimit failed")
	nnpMsg := []byte("prctl(PR_SET_NO_NEW_PRIVS) failed")
	execMsg := []byte("execve failed")

	// From here on: no allocations.
	if _, _, e := syscall.RawSyscall(syscall.SYS_FCNTL, 3, syscall.F_SETFD, syscall.FD_CLOEXEC); e != 0 {
		syscall.Exit(126)
	}
	if _, _, e := syscall.RawSyscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); e != 0 {
		syscall.RawSyscall(syscall.SYS_WRITE, 3, uintptr(unsafePtr(nnpMsg)), uintptr(len(nnpMsg)))
		syscall.Exit(126)
	}
	for i := 0; i < n; i++ {
		rl := syscall.Rlimit{Cur: lims[i].cur, Max: lims[i].max}
		if _, _, e := syscall.RawSyscall6(syscall.SYS_PRLIMIT64, 0, uintptr(lims[i].res),
			uintptr(unsafeRlimit(&rl)), 0, 0, 0); e != 0 {
			syscall.RawSyscall(syscall.SYS_WRITE, 3, uintptr(unsafePtr(errMsg)), uintptr(len(errMsg)))
			syscall.Exit(126)
		}
	}
	syscall.RawSyscall(syscall.SYS_EXECVE, uintptr(unsafeByte(pathp)),
		uintptr(unsafeArgv(&argv[0])), uintptr(unsafeArgv(&envv[0])))
	syscall.RawSyscall(syscall.SYS_WRITE, 3, uintptr(unsafePtr(execMsg)), uintptr(len(execMsg)))
	syscall.Exit(127)
}

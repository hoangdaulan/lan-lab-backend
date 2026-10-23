// Package cppjudge implements judge.Judger for single-file C++17 programs:
// compile with g++, run every test in the sandbox, compare tokens, and stop
// at the first non-AC test (BACKEND_CONTEXT.md §6, §19).
package cppjudge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/sandbox"
)

// Fixed by the user on 22/10/2026 (BACKEND_CONTEXT.md §19).
var DefaultCompileFlags = []string{"-std=c++17", "-O2", "-pipe", "-static", "-DONLINE_JUDGE"}

const (
	CompileTimeout   = 10 * time.Second
	compileMemory    = 1536 << 20 // cc1plus on large sources
	compileFileSize  = 256 << 20  // static binary + temporaries
	compileProcesses = 64         // g++ -> cc1plus, as, collect2, ld
	compileLogLimit  = 8 << 10    // compile output shown to students

	outputLimit    = 64 << 20 // stdout per test (RLIMIT_FSIZE)
	stderrKeep     = 4 << 10  // bytes of stderr read back for classification
	memoryHeadroom = 64 << 20 // RLIMIT_AS above memory_limit_mb, see classify
)

type Config struct {
	DataDir  string // DATA_DIR (read as root; must not be readable by runner uids)
	WorkDir  string // per-job scratch directories
	Compiler string // /usr/bin/g++
	Flags    []string
	UIDBase  int // runner uid/gid for slot i is UIDBase+i
	Slots    int // = WORKER_CONCURRENCY
}

type runFunc func(context.Context, sandbox.Spec) (sandbox.Result, error)

type Judger struct {
	cfg          Config
	compilerInfo string
	uids         chan int
	run          runFunc
	reap         func(uid, gid int) error
}

var _ judge.Judger = (*Judger)(nil)

// New checks the compiler and prepares one runner uid per slot.
func New(cfg Config) (*Judger, error) {
	if cfg.Compiler == "" {
		cfg.Compiler = "/usr/bin/g++"
	}
	if cfg.Flags == nil {
		cfg.Flags = DefaultCompileFlags
	}
	if cfg.Slots < 1 {
		cfg.Slots = 1
	}
	out, err := exec.Command(cfg.Compiler, "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("compiler %s: %w", cfg.Compiler, err)
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o711); err != nil {
		return nil, err
	}
	// Scratch directories left by a crashed worker are never reused.
	if stale, err := os.ReadDir(cfg.WorkDir); err == nil {
		for _, e := range stale {
			_ = os.RemoveAll(filepath.Join(cfg.WorkDir, e.Name()))
		}
	}
	j := &Judger{cfg: cfg, compilerInfo: strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]),
		uids: make(chan int, cfg.Slots), run: sandbox.Run, reap: sandbox.Reap}
	for i := 0; i < cfg.Slots; i++ {
		j.uids <- cfg.UIDBase + i
	}
	return j, nil
}

func (j *Judger) CompilerInfo() string { return j.compilerInfo }

// Judge returns an Outcome for anything the program did (CE included) and
// an error only for infrastructure failures, which the queue retries.
func (j *Judger) Judge(ctx context.Context, job judge.Job) (judge.Outcome, error) {
	if len(job.Tests) == 0 {
		return judge.Outcome{}, errors.New("job has no tests") // never AC without tests (§6)
	}
	var uid int
	select {
	case uid = <-j.uids:
	case <-ctx.Done():
		return judge.Outcome{}, ctx.Err()
	}
	defer func() { j.uids <- uid }()

	dir := filepath.Join(j.cfg.WorkDir, fmt.Sprintf("job%d-a%d", job.JobID, job.Attempt))
	if err := os.RemoveAll(dir); err != nil {
		return judge.Outcome{}, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return judge.Outcome{}, err
	}
	defer func() {
		_ = j.reap(uid, uid)
		_ = os.RemoveAll(dir)
	}()
	if err := os.Chown(dir, uid, uid); err != nil {
		return judge.Outcome{}, err
	}

	src, err := os.ReadFile(filepath.Join(j.cfg.DataDir, filepath.FromSlash(job.SourcePath)))
	if err != nil {
		return judge.Outcome{}, fmt.Errorf("read source: %w", err)
	}
	if err := writeOwned(filepath.Join(dir, "main.cpp"), src, uid); err != nil {
		return judge.Outcome{}, err
	}

	out := judge.Outcome{TotalTests: len(job.Tests), CompilerInfo: j.compilerInfo}
	ok, log, err := j.compile(ctx, dir, uid)
	if err != nil {
		return judge.Outcome{}, err
	}
	if !ok {
		out.Verdict, out.CompileOutput = judge.VerdictCE, log
		return out, nil
	}
	out.CompileOutput = log // warnings, if any

	for _, tc := range job.Tests {
		t, err := j.runTest(ctx, job, tc, dir, uid)
		if err != nil {
			return judge.Outcome{}, fmt.Errorf("test %d: %w", tc.ID, err)
		}
		out.Tests = append(out.Tests, t)
		if t.Verdict != judge.VerdictAC {
			out.Verdict = t.Verdict
			return out, nil // stop at the first non-AC test (§19)
		}
		out.PassedTests++
	}
	out.Verdict = judge.VerdictAC
	return out, nil
}

func writeOwned(path string, data []byte, uid int) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chown(path, uid, uid)
}

func (j *Judger) compile(ctx context.Context, dir string, uid int) (ok bool, log string, err error) {
	logf, err := os.OpenFile(filepath.Join(dir, "compile.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return false, "", err
	}
	defer logf.Close()
	args := append(append([]string{}, j.cfg.Flags...), "-o", "main", "main.cpp")
	res, err := j.run(ctx, sandbox.Spec{
		Path: j.cfg.Compiler, Args: args, Dir: dir,
		Env:    []string{"PATH=/usr/bin:/bin", "LANG=C", "TMPDIR=" + dir},
		Stdout: logf, Stderr: logf, UID: uid, GID: uid,
		Limits: sandbox.Limits{CPUTime: CompileTimeout, WallTime: CompileTimeout, AddressSpace: compileMemory,
			FileSize: compileFileSize, Processes: compileProcesses, OpenFiles: 256},
	})
	_ = j.reap(uid, uid)
	if err != nil {
		return false, "", fmt.Errorf("compile: %w", err)
	}
	log = readLimited(filepath.Join(dir, "compile.log"), compileLogLimit)
	switch {
	case res.WallTimedOut || res.Signal == syscall.SIGXCPU:
		return false, appendNote(log, fmt.Sprintf("compilation exceeded %s", CompileTimeout)), nil
	case res.Signaled || res.ExitCode != 0:
		return false, log, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "main")); err != nil {
		return false, appendNote(log, "compiler produced no executable"), nil
	}
	return true, log, nil
}

func appendNote(log, note string) string {
	if log != "" && !strings.HasSuffix(log, "\n") {
		log += "\n"
	}
	return log + note
}

// readLimited returns at most limit bytes, marking truncation.
func readLimited(path string, limit int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(b) > limit {
		return string(b[:limit]) + "\n... (truncated)"
	}
	return string(b)
}

func (j *Judger) runTest(ctx context.Context, job judge.Job, tc judge.TestCase, dir string, uid int) (judge.TestOutcome, error) {
	in, err := os.Open(filepath.Join(j.cfg.DataDir, filepath.FromSlash(tc.InputPath)))
	if err != nil {
		return judge.TestOutcome{}, fmt.Errorf("open input: %w", err)
	}
	defer in.Close()
	outPath, errPath := filepath.Join(dir, "out.txt"), filepath.Join(dir, "err.txt")
	outf, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return judge.TestOutcome{}, err
	}
	defer outf.Close()
	errf, err := os.OpenFile(errPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return judge.TestOutcome{}, err
	}
	defer errf.Close()

	tl := time.Duration(job.TimeLimitMs) * time.Millisecond
	mem := uint64(job.MemoryLimitMb) << 20
	res, err := j.run(ctx, sandbox.Spec{
		Path: "./main", Dir: dir, Stdin: in, Stdout: outf, Stderr: errf, UID: uid, GID: uid,
		Limits: sandbox.Limits{
			CPUTime: tl, WallTime: 2*tl + time.Second,
			AddressSpace: mem + memoryHeadroom, Stack: mem,
			FileSize: outputLimit, Processes: 1, OpenFiles: 16,
		},
	})
	_ = j.reap(uid, uid)
	if err != nil {
		return judge.TestOutcome{}, err
	}
	t := judge.TestOutcome{TestCaseID: tc.ID, TimeMs: int(res.CPUTime / time.Millisecond)}
	if res.MaxRSSKB > 0 {
		kb := res.MaxRSSKB
		t.MemoryKB = &kb
	}
	stderrTail := readLimited(errPath, stderrKeep)
	if v := classify(res, tl, job.MemoryLimitMb, stderrTail); v != "" {
		t.Verdict = v
		return t, nil
	}
	expected, err := os.Open(filepath.Join(j.cfg.DataDir, filepath.FromSlash(tc.OutputPath)))
	if err != nil {
		return judge.TestOutcome{}, fmt.Errorf("open expected output: %w", err)
	}
	defer expected.Close()
	got, err := os.Open(outPath)
	if err != nil {
		return judge.TestOutcome{}, err
	}
	defer got.Close()
	same, err := SameTokens(got, expected)
	if err != nil {
		return judge.TestOutcome{}, err
	}
	t.Verdict = judge.VerdictWA
	if same {
		t.Verdict = judge.VerdictAC
	}
	return t, nil
}

// classify decides TLE/MLE/RE from how the program ended; "" means it
// exited normally and its output must be compared.
//
//   - TLE: wall-clock limit hit, CPU time above the limit, or SIGXCPU.
//   - MLE: peak RSS above memory_limit_mb, or a crash after a failed
//     allocation. RLIMIT_AS is memory_limit + 64 MB, so a large malloc/new
//     fails instead of being killed; we recognise it from the C++ runtime's
//     "std::bad_alloc" message. A program that ignores a failed malloc and
//     then crashes is reported as RE — a known limit of setrlimit without
//     cgroups.
//   - RE: any other signal or non-zero exit code (includes SIGXFSZ when
//     stdout exceeds 64 MB).
func classify(res sandbox.Result, tl time.Duration, memMB int, stderr string) string {
	switch {
	case res.WallTimedOut, res.CPUTime > tl, res.Signaled && res.Signal == syscall.SIGXCPU:
		return judge.VerdictTLE
	case res.MaxRSSKB > int64(memMB)*1024:
		return judge.VerdictMLE
	case res.Signaled || res.ExitCode != 0:
		if strings.Contains(stderr, "std::bad_alloc") {
			return judge.VerdictMLE
		}
		return judge.VerdictRE
	}
	return ""
}

// isSpace matches C's isspace for the token comparison.
func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\v' || c == '\f'
}

// SameTokens compares two outputs token by token, ignoring differences in
// whitespace and line breaks (BACKEND_CONTEXT.md §6). It streams both
// inputs, so large outputs need no extra memory.
func SameTokens(a, b io.Reader) (bool, error) {
	ra, rb := newTokenReader(a), newTokenReader(b)
	for {
		ta, ea := ra.next()
		tb, eb := rb.next()
		if ea != nil && !errors.Is(ea, io.EOF) {
			return false, ea
		}
		if eb != nil && !errors.Is(eb, io.EOF) {
			return false, eb
		}
		if ea != nil || eb != nil {
			return ea != nil && eb != nil, nil // both ended together
		}
		if !bytes.Equal(ta, tb) {
			return false, nil
		}
	}
}

type tokenReader struct {
	r   io.Reader
	buf []byte
	pos int
	end int
	tok []byte
}

func newTokenReader(r io.Reader) *tokenReader {
	return &tokenReader{r: r, buf: make([]byte, 64<<10)}
}

func (t *tokenReader) fill() error {
	n, err := t.r.Read(t.buf)
	t.pos, t.end = 0, n
	if n > 0 {
		return nil
	}
	if err == nil {
		err = io.ErrNoProgress
	}
	return err
}

// next returns the next token (valid until the following call) or io.EOF.
func (t *tokenReader) next() ([]byte, error) {
	t.tok = t.tok[:0]
	for {
		if t.pos == t.end {
			if err := t.fill(); err != nil {
				if errors.Is(err, io.EOF) && len(t.tok) > 0 {
					return t.tok, nil
				}
				return nil, err
			}
		}
		c := t.buf[t.pos]
		t.pos++
		if isSpace(c) {
			if len(t.tok) > 0 {
				return t.tok, nil
			}
			continue
		}
		t.tok = append(t.tok, c)
	}
}

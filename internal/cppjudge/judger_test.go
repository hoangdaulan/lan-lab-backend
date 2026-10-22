package cppjudge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/sandbox"
)

func TestSameTokens(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"3\n", "3", true},
		{"1 2 3\n", "1\n2\n3\n\n", true},
		{"  1\t2\r\n", "1 2", true},
		{"", "", true},
		{"\n\n", "", true},
		{"1 2", "1 2 3", false},
		{"12", "1 2", false},
		{"abc", "abd", false},
		{"3.0", "3", false}, // no float tolerance (§6)
		{"1 2 3", "1 2", false},
	}
	for _, c := range cases {
		got, err := SameTokens(strings.NewReader(c.a), strings.NewReader(c.b))
		if err != nil || got != c.want {
			t.Errorf("SameTokens(%q, %q) = %v, %v; want %v", c.a, c.b, got, err, c.want)
		}
	}
	// Tokens split across tiny reads.
	long := strings.Repeat("1234567890 ", 20000)
	got, err := SameTokens(iotest.OneByteReader(strings.NewReader(long)), strings.NewReader(strings.ReplaceAll(long, " ", "\n")))
	if err != nil || !got {
		t.Errorf("long one-byte reads: %v, %v", got, err)
	}
	if _, err := SameTokens(iotest.ErrReader(errors.New("disk")), strings.NewReader("1")); err == nil {
		t.Error("read error not reported")
	}
}

func TestClassify(t *testing.T) {
	tl := time.Second
	cases := []struct {
		name   string
		res    sandbox.Result
		stderr string
		want   string
	}{
		{"normal exit", sandbox.Result{CPUTime: 100 * time.Millisecond, MaxRSSKB: 1000}, "", ""},
		{"wall timeout (sleep)", sandbox.Result{WallTimedOut: true, Signaled: true, Signal: syscall.SIGKILL}, "", judge.VerdictTLE},
		{"cpu over limit, exited", sandbox.Result{CPUTime: 1100 * time.Millisecond}, "", judge.VerdictTLE},
		{"SIGXCPU", sandbox.Result{Signaled: true, Signal: syscall.SIGXCPU, CPUTime: 900 * time.Millisecond}, "", judge.VerdictTLE},
		{"rss over limit", sandbox.Result{MaxRSSKB: 257 * 1024}, "", judge.VerdictMLE},
		{"bad_alloc", sandbox.Result{Signaled: true, Signal: syscall.SIGABRT, MaxRSSKB: 2000},
			"terminate called after throwing an instance of 'std::bad_alloc'", judge.VerdictMLE},
		{"segfault", sandbox.Result{Signaled: true, Signal: syscall.SIGSEGV}, "", judge.VerdictRE},
		{"FPE", sandbox.Result{Signaled: true, Signal: syscall.SIGFPE}, "", judge.VerdictRE},
		{"exit 1", sandbox.Result{ExitCode: 1}, "", judge.VerdictRE},
		{"output limit", sandbox.Result{Signaled: true, Signal: syscall.SIGXFSZ}, "", judge.VerdictRE},
	}
	for _, c := range cases {
		if got := classify(c.res, tl, 256, c.stderr); got != c.want {
			t.Errorf("%s: classify = %q, want %q", c.name, got, c.want)
		}
	}
}

// fakeSandbox scripts compile and per-test results without running anything.
type fakeSandbox struct {
	compileExit int
	compileLog  string
	tests       []sandbox.Result // result per test run, in order
	outputs     []string         // stdout per test run
	ran         int
	setupErr    error
}

func (f *fakeSandbox) run(_ context.Context, s sandbox.Spec) (sandbox.Result, error) {
	if f.setupErr != nil {
		return sandbox.Result{}, f.setupErr
	}
	if s.Path != "./main" { // compiler
		_, _ = s.Stdout.WriteString(f.compileLog)
		if f.compileExit == 0 {
			_ = os.WriteFile(filepath.Join(s.Dir, "main"), []byte("bin"), 0o700)
		}
		return sandbox.Result{ExitCode: f.compileExit}, nil
	}
	i := f.ran
	f.ran++
	_, _ = s.Stdout.WriteString(f.outputs[i])
	return f.tests[i], nil
}

// newFake builds a Judger over a temp DATA_DIR with n tests whose expected
// outputs are "1", "2", ...; the runner uid is the current user so chown
// works without root.
func newFake(t *testing.T, n int, fs *fakeSandbox) (*Judger, judge.Job) {
	t.Helper()
	data := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(data, "submissions"), 0o755))
	must(os.WriteFile(filepath.Join(data, "submissions/a.cpp"), []byte("int main(){}"), 0o644))
	job := judge.Job{JobID: 1, Attempt: 1, SourcePath: "submissions/a.cpp", TimeLimitMs: 1000, MemoryLimitMb: 256}
	for i := 1; i <= n; i++ {
		in, out := filepath.Join("t", strings.Repeat("x", i)+".in"), filepath.Join("t", strings.Repeat("x", i)+".out")
		must(os.MkdirAll(filepath.Join(data, "t"), 0o755))
		must(os.WriteFile(filepath.Join(data, in), []byte("q"), 0o644))
		must(os.WriteFile(filepath.Join(data, out), []byte(string(rune('0'+i))+"\n"), 0o644))
		job.Tests = append(job.Tests, judge.TestCase{ID: int64(i), InputPath: in, OutputPath: out})
	}
	j := &Judger{
		cfg:  Config{DataDir: data, WorkDir: t.TempDir(), Compiler: "/usr/bin/g++", Flags: DefaultCompileFlags, Slots: 1},
		uids: make(chan int, 1), run: fs.run, reap: func(int, int) error { return nil }, compilerInfo: "g++ test",
	}
	j.uids <- os.Getuid()
	return j, job
}

func ok() sandbox.Result { return sandbox.Result{CPUTime: 10 * time.Millisecond, MaxRSSKB: 1000} }

func TestJudgeAllAC(t *testing.T) {
	fs := &fakeSandbox{tests: []sandbox.Result{ok(), ok(), ok()}, outputs: []string{"1", "2\n", " 3 "}}
	j, job := newFake(t, 3, fs)
	o, err := j.Judge(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if o.Verdict != judge.VerdictAC || o.PassedTests != 3 || o.TotalTests != 3 || len(o.Tests) != 3 || o.CompilerInfo != "g++ test" {
		t.Fatalf("outcome = %+v", o)
	}
	if err := o.Validate(3); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(j.cfg.WorkDir); len(entries) != 0 {
		t.Errorf("work dir not cleaned: %v", entries)
	}
}

func TestJudgeStopsAtFirstFailure(t *testing.T) {
	// 5 tests, test 3 TLE: "2/5 TLE", tests 4-5 never run.
	tle := sandbox.Result{WallTimedOut: true, Signaled: true, Signal: syscall.SIGKILL}
	fs := &fakeSandbox{tests: []sandbox.Result{ok(), ok(), tle, ok(), ok()}, outputs: []string{"1", "2", "", "4", "5"}}
	j, job := newFake(t, 5, fs)
	o, err := j.Judge(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if o.Verdict != judge.VerdictTLE || o.PassedTests != 2 || o.TotalTests != 5 || len(o.Tests) != 3 || fs.ran != 3 {
		t.Fatalf("outcome = %+v, ran %d", o, fs.ran)
	}
	if err := o.Validate(5); err != nil {
		t.Fatal(err)
	}

	fs = &fakeSandbox{tests: []sandbox.Result{ok(), ok()}, outputs: []string{"9", "2"}}
	j, job = newFake(t, 2, fs)
	o, _ = j.Judge(context.Background(), job)
	if o.Verdict != judge.VerdictWA || o.PassedTests != 0 || fs.ran != 1 {
		t.Fatalf("WA outcome = %+v", o)
	}
}

func TestJudgeCompileError(t *testing.T) {
	fs := &fakeSandbox{compileExit: 1, compileLog: strings.Repeat("error: x\n", 2000)}
	j, job := newFake(t, 2, fs)
	o, err := j.Judge(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if o.Verdict != judge.VerdictCE || o.PassedTests != 0 || len(o.Tests) != 0 || fs.ran != 0 {
		t.Fatalf("outcome = %+v", o)
	}
	if len(o.CompileOutput) > compileLogLimit+32 || !strings.HasSuffix(o.CompileOutput, "(truncated)") {
		t.Errorf("compile output not truncated: %d bytes", len(o.CompileOutput))
	}
	if err := o.Validate(2); err != nil {
		t.Fatal(err)
	}
}

func TestJudgeInfrastructureErrors(t *testing.T) {
	fs := &fakeSandbox{setupErr: sandbox.ErrSetup}
	j, job := newFake(t, 1, fs)
	if _, err := j.Judge(context.Background(), job); !errors.Is(err, sandbox.ErrSetup) {
		t.Errorf("sandbox failure: %v, want ErrSetup", err)
	}
	fs = &fakeSandbox{tests: []sandbox.Result{ok()}, outputs: []string{"1"}}
	j, job = newFake(t, 1, fs)
	job.Tests[0].OutputPath = "t/missing.out"
	if _, err := j.Judge(context.Background(), job); err == nil {
		t.Error("missing expected output must be an infrastructure error")
	}
	job.Tests = nil
	if _, err := j.Judge(context.Background(), job); err == nil {
		t.Error("a job without tests must not be judged (never AC)")
	}
}

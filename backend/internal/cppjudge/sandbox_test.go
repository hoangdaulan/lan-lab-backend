//go:build sandbox

// Sandbox tests: real g++, real programs, real rlimits and namespaces. They
// need root with the worker's capabilities, so they run inside the worker
// container:
//
//	make test-sandbox
//
// (builds this package's test binary and runs it with `docker compose run
// worker`).
package cppjudge

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/sandbox"
)

func TestMain(m *testing.M) {
	sandbox.Main() // this test binary is also the sandbox helper
	os.Exit(m.Run())
}

const uidBase = 20000

var (
	dataDir string
	jdg     *Judger
	seq     int
)

func setup(t *testing.T) {
	t.Helper()
	if jdg != nil {
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("sandbox tests need root (run them in the worker container)")
	}
	// Shared by all tests (not t.TempDir: that is removed with the first
	// test and is 0700, which runner uids could not traverse — production
	// uses /var/judge with 0711).
	root, err := os.MkdirTemp("", "sandbox-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o711); err != nil {
		t.Fatal(err)
	}
	dataDir = filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "submissions"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Same protection the worker applies to DATA_DIR at start.
	if err := os.Chmod(dataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "submissions", "other.cpp"), []byte("SECRET SOURCE"), 0o644); err != nil {
		t.Fatal(err)
	}
	jdg, err = New(Config{DataDir: dataDir, WorkDir: filepath.Join(root, "work"), UIDBase: uidBase, Slots: 2})
	if err != nil {
		t.Fatal(err)
	}
}

// judgeSrc judges src against tests (input, expected) with the limits.
func judgeSrc(t *testing.T, src string, tests [][2]string, tlMs, memMB int) judge.Outcome {
	t.Helper()
	setup(t)
	seq++
	rel := fmt.Sprintf("submissions/p%d.cpp", seq)
	if err := os.WriteFile(filepath.Join(dataDir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	job := judge.Job{JobID: int64(seq), Attempt: 1, SourcePath: rel, TimeLimitMs: tlMs, MemoryLimitMb: memMB}
	dir := filepath.Join(dataDir, fmt.Sprintf("problems/%d/tests", seq))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, tc := range tests {
		in, out := fmt.Sprintf("problems/%d/tests/%d.in", seq, i+1), fmt.Sprintf("problems/%d/tests/%d.out", seq, i+1)
		_ = os.WriteFile(filepath.Join(dataDir, in), []byte(tc[0]), 0o644)
		_ = os.WriteFile(filepath.Join(dataDir, out), []byte(tc[1]), 0o644)
		job.Tests = append(job.Tests, judge.TestCase{ID: int64(i + 1), InputPath: in, OutputPath: out})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	o, err := jdg.Judge(ctx, job)
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if err := o.Validate(len(job.Tests)); err != nil {
		t.Fatalf("invalid outcome %+v: %v", o, err)
	}
	if n := runnerProcesses(); n != 0 {
		t.Errorf("%d runner processes left after judging", n)
	}
	return o
}

// runnerProcesses counts live processes owned by runner uids.
func runnerProcesses() int {
	entries, _ := os.ReadDir("/proc")
	n := 0
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				var real int
				fmt.Sscanf(strings.TrimPrefix(line, "Uid:"), "%d", &real)
				if real >= uidBase && real < uidBase+100 {
					n++
				}
			}
		}
	}
	return n
}

func expect(t *testing.T, o judge.Outcome, verdict string, passed int) {
	t.Helper()
	if o.Verdict != verdict || o.PassedTests != passed {
		t.Fatalf("got %d/%d %s, want %d/%d %s (tests %+v, compile %q)",
			o.PassedTests, o.TotalTests, o.Verdict, passed, o.TotalTests, verdict, o.Tests, o.CompileOutput)
	}
}

const aPlusB = `#include <cstdio>
int main(){long long a,b;if(scanf("%lld %lld",&a,&b)!=2)return 0;printf("%lld\n",a+b);}`

var sumTests = [][2]string{{"1 2\n", "3\n"}, {"10 20", "30"}, {"-5 5\n", "0\n"}}

func TestVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		tests   [][2]string
		mem     int
		verdict string
		passed  int
	}{
		{"AC", aPlusB, sumTests, 256, judge.VerdictAC, 3},
		{"WA on test 2 stops there", `#include <cstdio>
int main(){long long a,b;scanf("%lld %lld",&a,&b);printf("%lld\n",a==1?3:a-b);}`, sumTests, 256, judge.VerdictWA, 1},
		{"TLE busy loop", `int main(){volatile unsigned long x=0;for(;;)x++;}`, sumTests, 256, judge.VerdictTLE, 0},
		{"TLE sleeping (wall clock)", `#include <unistd.h>
int main(){sleep(30);}`, sumTests, 256, judge.VerdictTLE, 0},
		// Sizes come from the input so -O2 cannot elide the allocations.
		{"MLE huge new (bad_alloc)", `#include <cstdio>
#include <vector>
int main(){long long a,b;scanf("%lld %lld",&a,&b);std::vector<char> v((size_t)a<<30,1);
  long long s=0;for(size_t i=0;i<v.size();i+=4096)s+=v[i];printf("%lld\n",s+b);}`, sumTests, 256, judge.VerdictMLE, 0},
		{"MLE resident memory over limit", `#include <cstdlib>
#include <cstring>
#include <cstdio>
int main(){long long a,b;scanf("%lld %lld",&a,&b);size_t n=(size_t)a*150<<20;char*p=(char*)malloc(n);if(!p)return 2;
  memset(p,(int)b,n);long long s=0;for(size_t i=0;i<n;i+=4096)s+=p[i];printf("%lld\n",s);}`,
			sumTests, 128, judge.VerdictMLE, 0},
		{"RE segfault", `int main(){volatile int*p=0;*p=1;}`, sumTests, 256, judge.VerdictRE, 0},
		{"RE division by zero", `#include <cstdio>
int main(){int a,b;scanf("%d %d",&a,&b);int z=a-1;printf("%d\n",b/z);}`, sumTests, 256, judge.VerdictRE, 0},
		{"RE non-zero exit after 1 test", `#include <cstdio>
int main(){long long a,b;scanf("%lld %lld",&a,&b);if(a==10)return 3;printf("%lld\n",a+b);}`, sumTests, 256, judge.VerdictRE, 1},
		{"CE syntax error", `int main(){ return }`, sumTests, 256, judge.VerdictCE, 0},
		{"stack recursion within memory limit is fine", `#include <cstdio>
int f(int n){volatile char b[1024];b[0]=n;return n?f(n-1)+b[0]*0:0;}
int main(){long long a,b;scanf("%lld %lld",&a,&b);f(100000);printf("%lld\n",a+b);}`, sumTests, 256, judge.VerdictAC, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := judgeSrc(t, c.src, c.tests, 1000, c.mem)
			expect(t, o, c.verdict, c.passed)
			if c.verdict == judge.VerdictCE && !strings.Contains(o.CompileOutput, "error") {
				t.Errorf("CE without compiler message: %q", o.CompileOutput)
			}
			if !strings.HasPrefix(o.CompilerInfo, "g++ (Debian 12.2.0") {
				t.Errorf("compiler_info = %q", o.CompilerInfo)
			}
		})
	}
}

func TestIdentityAndPrivileges(t *testing.T) {
	src := `#include <cstdio>
#include <cstring>
#include <unistd.h>
int main(){
  char line[256]; unsigned long long capEff=1; int nnp=0;
  FILE*f=fopen("/proc/self/status","r");
  while(fgets(line,sizeof line,f)){
    if(!strncmp(line,"CapEff:",7)) sscanf(line+7,"%llx",&capEff);
    if(!strncmp(line,"NoNewPrivs:",11)) sscanf(line+11,"%d",&nnp);
  }
  printf("%s %s %s\n", getuid()>=20000&&getuid()<20100?"runner":"BAD-UID", capEff==0?"nocaps":"HAS-CAPS", nnp==1?"nnp":"NO-NNP");
}`
	o := judgeSrc(t, src, [][2]string{{"", "runner nocaps nnp"}}, 1000, 256)
	expect(t, o, judge.VerdictAC, 1)
}

func TestNoNetwork(t *testing.T) {
	pg, err := net.LookupHost("postgres")
	if err != nil || len(pg) == 0 {
		t.Skip("postgres not resolvable here")
	}
	// The worker itself can reach PostgreSQL ...
	if c, err := net.DialTimeout("tcp", pg[0]+":5432", 3*time.Second); err != nil {
		t.Fatalf("worker cannot reach postgres (test setup): %v", err)
	} else {
		c.Close()
	}
	// ... the student program cannot reach it, the Internet or loopback.
	src := `#include <cstdio>
#include <cstring>
#include <arpa/inet.h>
#include <sys/socket.h>
#include <unistd.h>
const char* try_(const char*ip,int port){
  int s=socket(AF_INET,SOCK_STREAM,0); if(s<0) return "blocked";
  sockaddr_in a{}; a.sin_family=AF_INET; a.sin_port=htons(port); inet_pton(AF_INET,ip,&a.sin_addr);
  int r=connect(s,(sockaddr*)&a,sizeof a); close(s); return r==0?"CONNECTED":"blocked";
}
int main(){char ip[64]; scanf("%63s",ip);
  printf("%s %s %s\n", try_(ip,5432), try_("1.1.1.1",80), try_("127.0.0.1",5432));}`
	o := judgeSrc(t, src, [][2]string{{pg[0], "blocked blocked blocked"}}, 2000, 256)
	expect(t, o, judge.VerdictAC, 1)
}

func TestCannotReadDataDir(t *testing.T) {
	setup(t)
	src := `#include <cstdio>
int main(){char p[512]; const char* r[3]; int i=0;
  while(i<3 && scanf("%511s",p)==1){ FILE*f=fopen(p,"r"); r[i++]= f?"READ":"blocked"; if(f)fclose(f);}
  printf("%s %s %s\n",r[0],r[1],r[2]);}`
	// Expected outputs of this very test, another student's source, and the
	// worker's own environment (it holds DATABASE_URL).
	self := filepath.Join(dataDir, fmt.Sprintf("problems/%d/tests/1.out", seq+1))
	other := filepath.Join(dataDir, "submissions/other.cpp")
	o := judgeSrc(t, src, [][2]string{{self + " " + other + " /proc/1/environ", "blocked blocked blocked"}}, 1000, 256)
	expect(t, o, judge.VerdictAC, 1)

	// Including a test file at compile time must fail too (compile runs as
	// the runner uid).
	inc := fmt.Sprintf("#include %q\nint main(){}", other)
	o = judgeSrc(t, inc, [][2]string{{"", ""}}, 1000, 256)
	expect(t, o, judge.VerdictCE, 0)
	if !strings.Contains(o.CompileOutput, "Permission denied") {
		t.Errorf("compile output = %q, want Permission denied", o.CompileOutput)
	}
}

func TestForkBombIsContained(t *testing.T) {
	src := `#include <cstdio>
#include <unistd.h>
#include <sys/wait.h>
int main(){int ok=0; for(int i=0;i<1000;i++){pid_t p=fork(); if(p==0){for(;;)fork();} if(p>0)ok++;}
  printf("%d\n",ok);}`
	o := judgeSrc(t, src, [][2]string{{"", "0"}}, 1000, 256)
	expect(t, o, judge.VerdictAC, 1) // every fork failed (RLIMIT_NPROC = 1)
}

func TestHugeOutputIsCut(t *testing.T) {
	src := `#include <cstdio>
#include <cstring>
int main(){static char b[1<<20]; memset(b,'x',sizeof b); for(int i=0;i<1024;i++) fwrite(b,1,sizeof b,stdout);}`
	o := judgeSrc(t, src, [][2]string{{"", "x"}}, 5000, 256)
	expect(t, o, judge.VerdictRE, 0) // SIGXFSZ at 64 MB
}

func TestConcurrentJobsUseSeparateSlots(t *testing.T) {
	setup(t)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { errs <- nil }()
			o := judgeSrcNoT(aPlusB)
			if o.Verdict != judge.VerdictAC {
				errs <- fmt.Errorf("verdict %s", o.Verdict)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

var seqMu = make(chan struct{}, 1)

// judgeSrcNoT is judgeSrc for goroutines (no *testing.T fatal calls).
func judgeSrcNoT(src string) judge.Outcome {
	seqMu <- struct{}{}
	seq++
	n := seq
	<-seqMu
	rel := fmt.Sprintf("submissions/c%d.cpp", n)
	_ = os.WriteFile(filepath.Join(dataDir, rel), []byte(src), 0o644)
	dir := filepath.Join(dataDir, fmt.Sprintf("problems/c%d", n))
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "1.in"), []byte("2 2"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "1.out"), []byte("4"), 0o644)
	job := judge.Job{JobID: int64(1000 + n), Attempt: 1, SourcePath: rel, TimeLimitMs: 1000, MemoryLimitMb: 256,
		Tests: []judge.TestCase{{ID: 1, InputPath: fmt.Sprintf("problems/c%d/1.in", n), OutputPath: fmt.Sprintf("problems/c%d/1.out", n)}}}
	o, err := jdg.Judge(context.Background(), job)
	if err != nil {
		return judge.Outcome{Verdict: "ERR " + err.Error()}
	}
	return o
}

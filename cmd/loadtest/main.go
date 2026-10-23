// Command loadtest simulates a class taking an exam against a running stack
// (phase 14): N students log in, open WebSockets (or poll), join a session
// and submit at the same moment; it reports latency, throughput, request
// count, bandwidth and database load. Results are appended as one JSON line
// to -out.
//
//	go run ./cmd/loadtest -students 50 -tests 15 -mode ws -label conc4 -out docs/results/runs.jsonl
//
// Needs a teacher account (-teacher-code / -teacher-password, default from
// .smoke.env variables), write access to ./data for the test files, and
// optionally -db (PostgreSQL URL, e.g. 127.0.0.1:5433) for server-side
// queue/judge times and database load.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
)

type config struct {
	base, teacherCode, teacherPassword, dataDir, dbURL, mode, label, out string
	students, submitters, tests                                          int
	pollInterval, timeout                                                time.Duration
}

func main() {
	var c config
	flag.StringVar(&c.base, "base", "http://localhost:8080", "API base URL")
	flag.StringVar(&c.teacherCode, "teacher-code", os.Getenv("SMOKE_TEACHER_CODE"), "teacher code")
	flag.StringVar(&c.teacherPassword, "teacher-password", os.Getenv("SMOKE_TEACHER_PASSWORD"), "teacher password")
	flag.StringVar(&c.dataDir, "data", "./data", "host path of DATA_DIR (test files are written here)")
	flag.StringVar(&c.dbURL, "db", os.Getenv("LOADTEST_DB"), "PostgreSQL URL for server-side metrics (optional)")
	flag.StringVar(&c.mode, "mode", "ws", "how students learn results: ws or poll")
	flag.StringVar(&c.label, "label", "", "label stored with the result")
	flag.StringVar(&c.out, "out", "", "append the JSON result to this file")
	flag.IntVar(&c.students, "students", 50, "students logged in")
	flag.IntVar(&c.submitters, "submit", 0, "students who submit (0 = all)")
	flag.IntVar(&c.tests, "tests", 15, "tests per problem")
	flag.DurationVar(&c.pollInterval, "poll-interval", time.Second, "polling interval (mode poll)")
	flag.DurationVar(&c.timeout, "timeout", 10*time.Minute, "give up after")
	flag.Parse()
	if c.submitters <= 0 || c.submitters > c.students {
		c.submitters = c.students
	}
	if c.mode != "ws" && c.mode != "poll" {
		fail(errors.New("-mode must be ws or poll"))
	}
	res, err := run(c)
	if err != nil {
		fail(err)
	}
	b, _ := json.Marshal(res)
	fmt.Println(string(b))
	if c.out != "" {
		_ = os.MkdirAll(filepath.Dir(c.out), 0o755)
		f, err := os.OpenFile(c.out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fail(err)
		}
		fmt.Fprintln(f, string(b))
		f.Close()
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "loadtest:", err)
	os.Exit(1)
}

// counters for HTTP traffic of the students (not the setup).
type traffic struct {
	requests, bytes atomic.Int64
	status          sync.Map // code -> *atomic.Int64
}

func (t *traffic) count(code int, n int64) {
	t.requests.Add(1)
	t.bytes.Add(n)
	v, _ := t.status.LoadOrStore(code, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

func (t *traffic) statuses() map[string]int64 {
	out := map[string]int64{}
	t.status.Range(func(k, v any) bool {
		out[fmt.Sprint(k)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

type api struct {
	base   string
	client *http.Client
	t      *traffic // nil = do not count
}

// do sends a request and decodes JSON into out (if not nil); it returns the
// status. Bytes counted: request body + response body + a fixed 200 bytes of
// headers each way (HTTP/1.1 estimate).
func (a api) do(method, path, token, ctype string, body []byte, out any) (int, error) {
	req, err := http.NewRequest(method, a.base+"/api/v1"+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		if a.t != nil {
			a.t.count(0, int64(len(body))+200)
		}
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if a.t != nil {
		a.t.count(resp.StatusCode, int64(len(body)+len(b))+400)
	}
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, err
		}
	}
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp.StatusCode, nil
}

func (a api) json(method, path, token string, in, out any) (int, error) {
	var body []byte
	if in != nil {
		body, _ = json.Marshal(in)
	}
	return a.do(method, path, token, "application/json", body, out)
}

func (a api) login(code, password string) (string, error) {
	var r struct {
		AccessToken string `json:"access_token"`
	}
	_, err := a.json("POST", "/auth/login", "", map[string]string{"code": code, "password": password}, &r)
	return r.AccessToken, err
}

func multipartFile(field, name string, content []byte) (string, []byte) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile(field, name)
	fw.Write(content)
	w.Close()
	return w.FormDataContentType(), buf.Bytes()
}

const program = `#include <cstdio>
int main(){long long a,b;if(scanf("%lld %lld",&a,&b)!=2)return 0;printf("%lld\n",a+b);}
`

type stats struct {
	N    int     `json:"n"`
	P50  float64 `json:"p50_ms"`
	P95  float64 `json:"p95_ms"`
	Max  float64 `json:"max_ms"`
	Mean float64 `json:"mean_ms"`
}

func summarize(ds []time.Duration) stats {
	if len(ds) == 0 {
		return stats{}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return stats{N: len(ds), P50: ms(ds[len(ds)/2]), P95: ms(ds[(len(ds)*95)/100]), Max: ms(ds[len(ds)-1]),
		Mean: ms(sum / time.Duration(len(ds)))}
}

type result struct {
	Label           string           `json:"label"`
	Mode            string           `json:"mode"`
	Students        int              `json:"students"`
	Submissions     int              `json:"submissions"`
	Tests           int              `json:"tests"`
	At              time.Time        `json:"at"`
	Login           stats            `json:"login"`
	Submit          stats            `json:"submit_202"`
	EndToEnd        stats            `json:"submit_to_result"`
	MakespanMs      float64          `json:"makespan_ms"`
	ThroughputPerS  float64          `json:"throughput_per_s"`
	Verdicts        map[string]int   `json:"verdicts"`
	WaitRequests    int64            `json:"wait_phase_requests"`
	WaitBytes       int64            `json:"wait_phase_bytes"`
	WSMessages      int64            `json:"ws_messages"`
	HTTPStatus      map[string]int64 `json:"http_status"`
	ServerQueueWait *stats           `json:"server_queue_wait,omitempty"`
	ServerJudge     *stats           `json:"server_judge,omitempty"`
	DBXactWait      int64            `json:"db_xact_during_wait,omitempty"`
	DBTuplesWait    int64            `json:"db_tuples_returned_during_wait,omitempty"`
	Errors          []string         `json:"errors,omitempty"`
}

func run(c config) (result, error) {
	res := result{Label: c.label, Mode: c.mode, Students: c.students, Submissions: c.submitters, Tests: c.tests,
		At: time.Now().UTC(), Verdicts: map[string]int{}}
	client := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 512, MaxConnsPerHost: 0}}
	setup := api{base: c.base, client: client}
	tr := &traffic{}
	st := api{base: c.base, client: client, t: tr}
	runID := time.Now().Format("150405")

	// --- setup (not measured) ---
	tt, err := setup.login(c.teacherCode, c.teacherPassword)
	if err != nil {
		return res, fmt.Errorf("teacher login: %w", err)
	}
	var exam struct{ ID int64 }
	if _, err := setup.json("POST", "/exams", tt, map[string]string{"title": "Load " + runID + " " + c.label}, &exam); err != nil {
		return res, err
	}
	now := time.Now().UTC()
	var sess struct{ ID int64 }
	if _, err := setup.json("POST", fmt.Sprintf("/exams/%d/sessions", exam.ID), tt, map[string]any{
		"name": "load", "starts_at": now.Add(-time.Minute), "ends_at": now.Add(2 * time.Hour)}, &sess); err != nil {
		return res, err
	}
	var prob struct {
		ID      int64  `json:"id"`
		TestDir string `json:"test_dir"`
	}
	if _, err := setup.json("POST", fmt.Sprintf("/exams/%d/problems", exam.ID), tt, map[string]any{
		"code": "A", "title": "Sum", "statement": "a+b", "time_limit_ms": 1000, "memory_limit_mb": 256}, &prob); err != nil {
		return res, err
	}
	dir := filepath.Join(c.dataDir, filepath.FromSlash(prob.TestDir))
	for i := 1; i <= c.tests; i++ {
		a, b := rand.Int64N(1e12), rand.Int64N(1e12)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.in", i)), []byte(fmt.Sprintf("%d %d\n", a, b)), 0o644); err != nil {
			return res, err
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.out", i)), []byte(fmt.Sprintf("%d\n", a+b)), 0o644); err != nil {
			return res, err
		}
	}
	if _, err := setup.json("POST", fmt.Sprintf("/problems/%d/rejudge", prob.ID), tt, nil, nil); err != nil {
		return res, err
	}
	var csv strings.Builder
	csv.WriteString("code,full_name\n")
	codes := make([]string, c.students)
	for i := range codes {
		codes[i] = fmt.Sprintf("LT%s%03d", runID, i)
		fmt.Fprintf(&csv, "%s,Load Student %d\n", codes[i], i)
	}
	ct, body := multipartFile("file", "students.csv", []byte(csv.String()))
	if _, err := setup.do("POST", "/students/import", tt, ct, body, nil); err != nil {
		return res, err
	}

	// --- 1. everybody logs in at once ---
	tokens := make([]string, c.students)
	loginLat := make([]time.Duration, c.students)
	if err := parallel(c.students, func(i int) error {
		start := time.Now()
		tok, err := st.login(codes[i], codes[i])
		loginLat[i] = time.Since(start)
		tokens[i] = tok
		if err != nil {
			return err
		}
		_, err = st.json("POST", fmt.Sprintf("/sessions/%d/join", sess.ID), tok, nil, nil)
		return err
	}); err != nil {
		return res, err
	}
	res.Login = summarize(loginLat)

	// --- 2. WebSockets (mode ws) ---
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	type outcome struct {
		at      time.Time
		verdict string
	}
	results := make([]chan outcome, c.students)
	for i := range results {
		results[i] = make(chan outcome, 1)
	}
	var subIDs sync.Map // submission id -> student index
	var wsMsgs, wsBytes atomic.Int64
	if c.mode == "ws" {
		wsURL := "ws" + strings.TrimPrefix(c.base, "http") + "/api/v1/ws"
		if err := parallel(c.students, func(i int) error {
			conn, _, err := websocket.Dial(ctx, wsURL, nil)
			if err != nil {
				return err
			}
			conn.SetReadLimit(1 << 20)
			for _, m := range []map[string]any{
				{"type": "auth", "data": map[string]string{"token": tokens[i]}},
				{"type": "subscribe", "data": map[string]int64{"session_id": sess.ID}},
			} {
				b, _ := json.Marshal(m)
				if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
					return err
				}
			}
			go func() {
				defer conn.CloseNow()
				for {
					_, b, err := conn.Read(ctx)
					if err != nil {
						return
					}
					wsMsgs.Add(1)
					wsBytes.Add(int64(len(b)) + 4) // + frame header
					var m struct {
						Type string `json:"type"`
						Data struct {
							ID     int64 `json:"id"`
							Result *struct {
								Status  string  `json:"status"`
								Verdict *string `json:"verdict"`
							} `json:"result"`
						} `json:"data"`
					}
					if json.Unmarshal(b, &m) != nil || m.Type != "run.updated" || m.Data.Result == nil {
						continue
					}
					if s := m.Data.Result.Status; s == "completed" || s == "system_error" {
						v := s
						if m.Data.Result.Verdict != nil {
							v = *m.Data.Result.Verdict
						}
						if idx, ok := subIDs.Load(m.Data.ID); ok {
							select {
							case results[idx.(int)] <- outcome{time.Now(), v}:
							default:
							}
						}
					}
				}
			}()
			return nil
		}); err != nil {
			return res, fmt.Errorf("websocket: %w", err)
		}
		time.Sleep(500 * time.Millisecond) // let subscriptions settle
	}

	// --- 3. submit at the same moment ---
	submitLat := make([]time.Duration, c.submitters)
	submitAt := make([]time.Time, c.submitters)
	ids := make([]int64, c.submitters)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	var subErrs []string
	var mu sync.Mutex
	for i := 0; i < c.submitters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ct, body := multipartFile("file", "main.cpp", []byte(program))
			<-gate
			submitAt[i] = time.Now()
			var s struct{ ID int64 }
			_, err := st.do("POST", fmt.Sprintf("/sessions/%d/problems/%d/submissions", sess.ID, prob.ID), tokens[i], ct, body, &s)
			submitLat[i] = time.Since(submitAt[i])
			if err != nil {
				mu.Lock()
				subErrs = append(subErrs, err.Error())
				mu.Unlock()
				return
			}
			ids[i] = s.ID
			subIDs.Store(s.ID, i)
		}(i)
	}
	dbBefore, _ := dbCounters(c.dbURL)
	reqBefore, bytesBefore := tr.requests.Load(), tr.bytes.Load()
	first := time.Now()
	close(gate)
	wg.Wait()
	res.Submit = summarize(submitLat)
	res.Errors = subErrs

	// --- 4. wait for results ---
	if c.mode == "poll" {
		for i := 0; i < c.submitters; i++ {
			if ids[i] == 0 {
				continue
			}
			go func(i int) {
				t := time.NewTicker(c.pollInterval)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
					}
					var s struct {
						Result *struct {
							Status  string  `json:"status"`
							Verdict *string `json:"verdict"`
						} `json:"result"`
					}
					if _, err := st.json("GET", fmt.Sprintf("/submissions/%d", ids[i]), tokens[i], nil, &s); err != nil || s.Result == nil {
						continue
					}
					if s.Result.Status == "completed" || s.Result.Status == "system_error" {
						v := s.Result.Status
						if s.Result.Verdict != nil {
							v = *s.Result.Verdict
						}
						results[i] <- outcome{time.Now(), v}
						return
					}
				}
			}(i)
		}
	}
	var e2e []time.Duration
	last := first
	for i := 0; i < c.submitters; i++ {
		if ids[i] == 0 {
			continue
		}
		select {
		case o := <-results[i]:
			e2e = append(e2e, o.at.Sub(submitAt[i]))
			res.Verdicts[o.verdict]++
			if o.at.After(last) {
				last = o.at
			}
		case <-ctx.Done():
			return res, fmt.Errorf("timeout waiting for results (%d/%d done)", len(e2e), c.submitters)
		}
	}
	res.EndToEnd = summarize(e2e)
	res.MakespanMs = float64(last.Sub(first).Microseconds()) / 1000
	if res.MakespanMs > 0 {
		res.ThroughputPerS = float64(len(e2e)) / (res.MakespanMs / 1000)
	}
	res.WaitRequests = tr.requests.Load() - reqBefore
	res.WaitBytes = tr.bytes.Load() - bytesBefore + wsBytes.Load()
	res.WSMessages = wsMsgs.Load()
	res.HTTPStatus = tr.statuses()
	if dbAfter, err := dbCounters(c.dbURL); err == nil && dbBefore != nil {
		res.DBXactWait = dbAfter[0] - dbBefore[0]
		res.DBTuplesWait = dbAfter[1] - dbBefore[1]
	}
	if c.dbURL != "" {
		qw, jd, err := serverTimes(c.dbURL, ids)
		if err == nil {
			res.ServerQueueWait, res.ServerJudge = &qw, &jd
		}
	}
	return res, nil
}

func parallel(n int, f func(i int) error) error {
	var wg sync.WaitGroup
	errs := make(chan error, n)
	gate := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			if err := f(i); err != nil {
				errs <- err
			}
		}(i)
	}
	close(gate)
	wg.Wait()
	close(errs)
	return <-errs
}

// dbCounters returns [xact_commit+xact_rollback, tup_returned+tup_fetched]
// of the database.
func dbCounters(url string) ([]int64, error) {
	if url == "" {
		return nil, errors.New("no db")
	}
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())
	_, _ = conn.Exec(context.Background(), "SELECT pg_stat_clear_snapshot()")
	var x, t int64
	err = conn.QueryRow(context.Background(), `SELECT xact_commit + xact_rollback, tup_returned + tup_fetched
		FROM pg_stat_database WHERE datname = current_database()`).Scan(&x, &t)
	return []int64{x, t}, err
}

// serverTimes: queue wait (run created -> started) and judge time (started
// -> finished) of the current runs of these submissions.
func serverTimes(url string, ids []int64) (stats, stats, error) {
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		return stats{}, stats{}, err
	}
	defer conn.Close(context.Background())
	rows, err := conn.Query(context.Background(), `SELECT started_at - created_at, finished_at - started_at
		FROM judge_runs WHERE submission_id = ANY($1) AND status = 'completed'`, ids)
	if err != nil {
		return stats{}, stats{}, err
	}
	defer rows.Close()
	var qw, jd []time.Duration
	for rows.Next() {
		var a, b time.Duration
		if err := rows.Scan(&a, &b); err != nil {
			return stats{}, stats{}, err
		}
		qw, jd = append(qw, a), append(jd, b)
	}
	return summarize(qw), summarize(jd), rows.Err()
}

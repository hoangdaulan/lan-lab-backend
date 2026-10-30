package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/wire"
)

// Executor is the worker side that judges pushed jobs (*judge.Worker).
type Executor interface {
	ID() string
	Concurrency() int
	// SetPushed(true) stops claiming by polling while connected.
	SetPushed(on bool)
	// Acquire waits for a judging slot. The dispatcher never sends more
	// jobs than the worker's slots, but jobs claimed by polling just before
	// connecting may still hold some for a moment.
	Acquire(ctx context.Context) error
	Release()
	JobLog(job judge.Job) *slog.Logger
	Execute(ctx context.Context, job judge.Job, log *slog.Logger) (judge.Outcome, error)
	Record(ctx context.Context, job judge.Job, o judge.Outcome, jerr error, took time.Duration, log *slog.Logger) error
}

// Client keeps one connection to the dispatcher, reconnecting with backoff.
// While disconnected the worker polls the database as before.
type Client struct {
	// Resolve returns the dispatcher's host:port; called before each dial
	// so "auto" mode can rediscover a server that moved.
	Resolve func(ctx context.Context) (string, error)
	// Token is sent in HELLO (DISPATCH_TOKEN).
	Token string
	ex    Executor
	log   *slog.Logger

	// ResultTimeout bounds the wait for RESULT_OK before the worker writes
	// the result to the database itself.
	ResultTimeout time.Duration

	mu   sync.Mutex
	cur  *clientConn
	jobs sync.WaitGroup
}

type clientConn struct {
	conn net.Conn
	wmu  sync.Mutex
	mu   sync.Mutex
	wait map[int64]chan resultOKMsg // RESULT sent, RESULT_OK pending
}

// NewClient dials a fixed address.
func NewClient(addr string, ex Executor, log *slog.Logger) *Client {
	return &Client{
		Resolve:       func(context.Context) (string, error) { return addr, nil },
		ex:            ex,
		log:           log.With("component", "dispatch-client"),
		ResultTimeout: 15 * time.Second,
	}
}

// Run connects and serves until ctx ends, then waits for pushed jobs that
// are still being judged.
func (c *Client) Run(ctx context.Context) {
	defer c.jobs.Wait()
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		addr, err := c.Resolve(ctx)
		if err == nil {
			err = c.session(ctx, addr)
		}
		c.ex.SetPushed(false)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		c.log.Warn("dispatcher unavailable, polling the database until reconnect",
			"addr", addr, "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

func (c *Client) session(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	cc := &clientConn{conn: conn, wait: map[int64]chan resultOKMsg{}}
	defer func() {
		conn.Close()
		c.setCurrent(nil)
		cc.failWaiters()
	}()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	// Handshake: HELLO → WELCOME (or ERROR).
	if err := cc.send(wire.Hello, helloMsg{WorkerID: c.ex.ID(), Slots: c.ex.Concurrency(), Version: ProtocolVersion, Token: c.Token}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, payload, err := wire.Read(conn)
	if err != nil {
		return err
	}
	if typ == wire.Error {
		var e errorMsg
		_ = wire.Decode(payload, &e)
		return fmt.Errorf("dispatcher refused: %s", e.Message)
	}
	var w welcomeMsg
	if typ != wire.Welcome || wire.Decode(payload, &w) != nil {
		return fmt.Errorf("expected WELCOME, got %s", typ)
	}
	hb := time.Duration(w.HeartbeatMs) * time.Millisecond
	if hb < 100*time.Millisecond {
		hb = 5 * time.Second
	}
	c.setCurrent(cc)
	c.ex.SetPushed(true)
	c.log.Info("connected to dispatcher", "addr", addr, "server", w.Server, "heartbeat", hb)

	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		t := time.NewTicker(hb)
		defer t.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-t.C:
				if cc.send(wire.Ping, nil) != nil {
					conn.Close()
					return
				}
			}
		}
	}()

	for {
		// The server answers every PING, so 3 silent intervals mean the
		// link or the server is gone.
		_ = conn.SetReadDeadline(time.Now().Add(3 * hb))
		typ, payload, err := wire.Read(conn)
		if err != nil {
			return err
		}
		switch typ {
		case wire.JobMsg:
			var job judge.Job
			if err := wire.Decode(payload, &job); err != nil {
				return fmt.Errorf("bad JOB: %w", err)
			}
			if err := cc.send(wire.Ack, ackMsg{JobID: job.JobID, Accepted: true}); err != nil {
				// Never acknowledged: the dispatcher requeues it.
				return err
			}
			c.jobs.Add(1)
			go c.judge(ctx, job)
		case wire.ResultOK:
			var r resultOKMsg
			if err := wire.Decode(payload, &r); err != nil {
				return fmt.Errorf("bad RESULT_OK: %w", err)
			}
			cc.deliver(r)
		case wire.Ping:
			if err := cc.send(wire.Pong, nil); err != nil {
				return err
			}
		case wire.Pong:
		case wire.Error:
			var e errorMsg
			_ = wire.Decode(payload, &e)
			return fmt.Errorf("dispatcher error: %s", e.Message)
		default:
			return fmt.Errorf("unexpected %s", typ)
		}
	}
}

// judge runs one pushed job and reports it; if the dispatcher cannot take
// the result, the worker writes it to the database directly.
func (c *Client) judge(ctx context.Context, job judge.Job) {
	defer c.jobs.Done()
	log := c.ex.JobLog(job).With("via", "tcp")
	log.Info("job received", "queue_wait_ms", time.Since(job.QueuedAt).Milliseconds(), "tests", len(job.Tests))
	// Waiting must not be cut short by shutdown: the job is ours (ACKed),
	// and the slot frees as soon as the polled job finishes.
	if err := c.ex.Acquire(context.WithoutCancel(ctx)); err != nil {
		return
	}
	start := time.Now()
	outcome, jerr := c.ex.Execute(ctx, job, log)
	took := time.Since(start)
	c.ex.Release()

	r := resultMsg{JobID: job.JobID, JudgeMs: took.Milliseconds()}
	if jerr != nil {
		r.Error = jerr.Error()
	} else {
		r.Outcome = &outcome
	}
	status, err := c.report(r)
	switch status {
	case statusSaved, statusDropped:
		log.Info("result delivered", "status", status, "verdict", outcome.Verdict, "judge_ms", took.Milliseconds())
		return
	}
	log.Warn("dispatcher did not store the result, writing it to the database", "status", status, "err", err)
	if err := c.ex.Record(ctx, job, outcome, jerr, took, log); err != nil {
		log.Error("record result", "err", err)
	}
}

// report sends RESULT on the current connection and waits for RESULT_OK.
func (c *Client) report(r resultMsg) (string, error) {
	cc := c.current()
	if cc == nil {
		return "", errors.New("not connected")
	}
	ch := cc.expect(r.JobID)
	if err := cc.send(wire.Result, r); err != nil {
		return "", err
	}
	select {
	case ok, open := <-ch:
		if !open {
			return "", errors.New("connection lost before RESULT_OK")
		}
		return ok.Status, nil
	case <-time.After(c.ResultTimeout):
		return "", errors.New("RESULT_OK timeout")
	}
}

func (c *Client) current() *clientConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *Client) setCurrent(cc *clientConn) {
	c.mu.Lock()
	c.cur = cc
	c.mu.Unlock()
}

func (cc *clientConn) send(t wire.Type, v any) error { return writeFrame(cc.conn, &cc.wmu, t, v) }

func (cc *clientConn) expect(jobID int64) chan resultOKMsg {
	ch := make(chan resultOKMsg, 1)
	cc.mu.Lock()
	cc.wait[jobID] = ch
	cc.mu.Unlock()
	return ch
}

func (cc *clientConn) deliver(r resultOKMsg) {
	cc.mu.Lock()
	ch := cc.wait[r.JobID]
	delete(cc.wait, r.JobID)
	cc.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}

func (cc *clientConn) failWaiters() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	for id, ch := range cc.wait {
		close(ch)
		delete(cc.wait, id)
	}
}

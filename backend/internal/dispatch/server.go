package dispatch

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/wire"
)

// Repository is the part of the judge queue the dispatcher uses.
type Repository interface {
	Claim(ctx context.Context, workerID string, lease time.Duration) (judge.Job, error)
	Complete(ctx context.Context, job judge.Job, o judge.Outcome) error
	Fail(ctx context.Context, job judge.Job, cause string, policy judge.RetryPolicy) (bool, error)
}

// ServerConfig tunes the dispatcher; zero values take the defaults.
type ServerConfig struct {
	Name         string        // sent in WELCOME
	Token        string        // shared secret workers must send in HELLO (empty: none)
	Lease        time.Duration // lease of claimed jobs (WORKER_LEASE)
	Retry        judge.RetryPolicy
	PollInterval time.Duration // fallback scan when no NOTIFY arrives (1 s)
	Heartbeat    time.Duration // PING interval asked of workers (5 s); 3 missed = dead
	AckTimeout   time.Duration // JOB without ACK is failed back to the queue (10 s)
}

// Server accepts worker connections and pushes them jobs.
type Server struct {
	repo Repository
	cfg  ServerConfig
	log  *slog.Logger

	mu    sync.Mutex
	peers map[string]*peer // by worker id
	wake  chan struct{}
}

type peer struct {
	conn  net.Conn
	id    string
	slots int
	wmu   sync.Mutex
	// guarded by Server.mu
	inflight map[int64]*pending
	closed   bool
}

type pending struct {
	job    judge.Job
	sentAt time.Time
	acked  bool
}

func NewServer(repo Repository, cfg ServerConfig, log *slog.Logger) *Server {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 5 * time.Second
	}
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = 10 * time.Second
	}
	if cfg.Name == "" {
		cfg.Name = "lan-lab-api"
	}
	return &Server{repo: repo, cfg: cfg, log: log.With("component", "dispatch"),
		peers: map[string]*peer{}, wake: make(chan struct{}, 1)}
}

// Wake asks the dispatch loop to look for queued jobs now (non-blocking).
func (s *Server) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Workers returns the connected worker ids and their in-flight job counts.
func (s *Server) Workers() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.peers))
	for id, p := range s.peers {
		out[id] = len(p.inflight)
	}
	return out
}

// Serve accepts connections on ln until ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	s.log.Info("dispatcher listening", "addr", ln.Addr().String())
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

// Run is the dispatch loop: whenever a job may be queued (NOTIFY wake, a
// result freed a slot, or the poll tick) it claims jobs for workers with
// free slots and pushes them.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.PollInterval)
	defer t.Stop()
	for {
		s.dispatch(ctx)
		s.expireAcks(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-t.C:
		}
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	log := s.log.With("remote", conn.RemoteAddr().String())
	p, err := s.handshake(conn)
	if err != nil {
		log.Warn("worker handshake failed", "err", err)
		_ = writeFrame(conn, nil, wire.Error, errorMsg{Message: err.Error()})
		conn.Close()
		return
	}
	log = log.With("worker_id", p.id)
	s.register(p, log)
	log.Info("worker connected", "slots", p.slots)
	s.Wake()

	// Closing the connection on shutdown unblocks the read below.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	err = s.readLoop(ctx, p, log)
	conn.Close()
	unacked := s.unregister(p)
	for _, pd := range unacked {
		s.failJob(pd.job, "dispatch: worker disconnected before ACK", log)
	}
	log.Info("worker disconnected", "err", err, "requeued_unacked", len(unacked))
}

func (s *Server) handshake(conn net.Conn) (*peer, error) {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, payload, err := wire.Read(conn)
	if err != nil {
		return nil, err
	}
	if typ != wire.Hello {
		return nil, fmt.Errorf("expected HELLO, got %s", typ)
	}
	var h helloMsg
	if err := wire.Decode(payload, &h); err != nil {
		return nil, fmt.Errorf("bad HELLO: %w", err)
	}
	switch {
	case h.Version != ProtocolVersion:
		return nil, fmt.Errorf("unsupported protocol version %d", h.Version)
	case subtle.ConstantTimeCompare([]byte(h.Token), []byte(s.cfg.Token)) != 1:
		return nil, errors.New("invalid token")
	case h.WorkerID == "" || len(h.WorkerID) > 100:
		return nil, errors.New("worker_id must be 1-100 characters")
	case h.Slots < 1 || h.Slots > 64:
		return nil, errors.New("slots must be 1-64")
	}
	p := &peer{conn: conn, id: h.WorkerID, slots: h.Slots, inflight: map[int64]*pending{}}
	err = p.send(wire.Welcome, welcomeMsg{Server: s.cfg.Name, HeartbeatMs: s.cfg.Heartbeat.Milliseconds()})
	return p, err
}

// register adds p, replacing a stale connection with the same worker id
// (the worker reconnected before the old socket timed out).
func (s *Server) register(p *peer, log *slog.Logger) {
	s.mu.Lock()
	old := s.peers[p.id]
	s.peers[p.id] = p
	s.mu.Unlock()
	if old != nil {
		log.Warn("replacing previous connection of this worker")
		old.conn.Close()
	}
}

// unregister removes p and returns the jobs it never acknowledged.
func (s *Server) unregister(p *peer) []*pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.closed = true
	if s.peers[p.id] == p {
		delete(s.peers, p.id)
	}
	var unacked []*pending
	for id, pd := range p.inflight {
		if !pd.acked {
			unacked = append(unacked, pd)
			delete(p.inflight, id)
		}
	}
	// Acknowledged jobs are still being judged: the worker renews their
	// lease and writes the result itself if it cannot reach us.
	return unacked
}

func (s *Server) readLoop(ctx context.Context, p *peer, log *slog.Logger) error {
	for {
		_ = p.conn.SetReadDeadline(time.Now().Add(3 * s.cfg.Heartbeat))
		typ, payload, err := wire.Read(p.conn)
		if err != nil {
			return err
		}
		switch typ {
		case wire.Ping:
			if err := p.send(wire.Pong, nil); err != nil {
				return err
			}
		case wire.Pong:
		case wire.Ack:
			var a ackMsg
			if err := wire.Decode(payload, &a); err != nil {
				return s.protocolError(p, err)
			}
			s.onAck(p, a, log)
		case wire.Result:
			var r resultMsg
			if err := wire.Decode(payload, &r); err != nil {
				return s.protocolError(p, err)
			}
			// The database write can take a while; keep reading PINGs.
			go s.onResult(ctx, p, r, log)
		default:
			return s.protocolError(p, fmt.Errorf("unexpected %s", typ))
		}
	}
}

func (s *Server) protocolError(p *peer, err error) error {
	_ = p.send(wire.Error, errorMsg{Message: err.Error()})
	return fmt.Errorf("protocol error: %w", err)
}

func (s *Server) onAck(p *peer, a ackMsg, log *slog.Logger) {
	s.mu.Lock()
	pd := p.inflight[a.JobID]
	if pd != nil {
		if a.Accepted {
			pd.acked = true
		} else {
			delete(p.inflight, a.JobID)
		}
	}
	s.mu.Unlock()
	if pd == nil {
		log.Warn("ACK for unknown job", "job_id", a.JobID)
		return
	}
	if !a.Accepted {
		s.failJob(pd.job, "dispatch: worker refused job: "+a.Reason, log)
	}
}

func (s *Server) onResult(ctx context.Context, p *peer, r resultMsg, log *slog.Logger) {
	s.mu.Lock()
	pd := p.inflight[r.JobID]
	delete(p.inflight, r.JobID)
	s.mu.Unlock()
	reply := resultOKMsg{JobID: r.JobID}
	if pd == nil {
		reply.Status = statusUnknown
	} else {
		reply.Status, reply.Detail = s.record(ctx, pd.job, r, log.With("job_id", r.JobID))
	}
	if err := p.send(wire.ResultOK, reply); err != nil {
		log.Warn("send RESULT_OK", "job_id", r.JobID, "err", err)
	}
	s.Wake() // a slot is free
}

// record writes a worker's result, like judge.Worker.Record but on behalf
// of a remote worker.
func (s *Server) record(ctx context.Context, job judge.Job, r resultMsg, log *slog.Logger) (status, detail string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var err error
	switch {
	case r.Error != "":
		var final bool
		final, err = s.repo.Fail(wctx, job, r.Error, s.cfg.Retry)
		if err == nil {
			log.Warn("infrastructure failure reported by worker", "cause", r.Error, "final", final)
		}
	case r.Outcome == nil:
		err = errors.New("RESULT without outcome or error")
	default:
		err = s.repo.Complete(wctx, job, *r.Outcome)
		if err == nil {
			log.Info("job done", "verdict", r.Outcome.Verdict, "passed", r.Outcome.PassedTests,
				"total", r.Outcome.TotalTests, "judge_ms", r.JudgeMs)
		}
	}
	switch {
	case err == nil:
		return statusSaved, ""
	case errors.Is(err, judge.ErrLeaseLost), errors.Is(err, judge.ErrStale):
		log.Warn("result dropped", "reason", err)
		return statusDropped, err.Error()
	default:
		log.Error("record result", "err", err)
		return statusError, err.Error()
	}
}

// dispatch claims jobs while some worker has a free slot.
func (s *Server) dispatch(ctx context.Context) {
	for ctx.Err() == nil {
		p := s.pickPeer()
		if p == nil {
			return
		}
		job, err := s.repo.Claim(ctx, p.id, s.cfg.Lease)
		if errors.Is(err, judge.ErrNoJob) {
			return
		}
		if err != nil {
			if ctx.Err() == nil {
				s.log.Error("claim for worker", "worker_id", p.id, "err", err)
			}
			return
		}
		s.mu.Lock()
		closed := p.closed
		if !closed {
			p.inflight[job.JobID] = &pending{job: job, sentAt: time.Now()}
		}
		s.mu.Unlock()
		log := s.log.With("worker_id", p.id, "job_id", job.JobID, "submission_id", job.SubmissionID)
		if closed {
			s.failJob(job, "dispatch: worker disconnected", log)
			continue
		}
		if err := p.send(wire.JobMsg, job); err != nil {
			// The read loop sees the closed socket and requeues the job.
			log.Warn("send JOB failed", "err", err)
			p.conn.Close()
			continue
		}
		log.Info("job dispatched", "queue_wait_ms", time.Since(job.QueuedAt).Milliseconds())
	}
}

// pickPeer returns the connected worker with the most free slots.
func (s *Server) pickPeer() *peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *peer
	bestFree := 0
	for _, p := range s.peers {
		if free := p.slots - len(p.inflight); !p.closed && free > bestFree {
			best, bestFree = p, free
		}
	}
	return best
}

// expireAcks fails jobs whose JOB frame was never acknowledged and drops
// the connection that swallowed them.
func (s *Server) expireAcks(ctx context.Context) {
	cutoff := time.Now().Add(-s.cfg.AckTimeout)
	var expired []*pending
	var bad []*peer
	s.mu.Lock()
	for _, p := range s.peers {
		n := len(expired)
		for id, pd := range p.inflight {
			if !pd.acked && pd.sentAt.Before(cutoff) {
				expired = append(expired, pd)
				delete(p.inflight, id)
			}
		}
		if len(expired) > n {
			bad = append(bad, p)
		}
	}
	s.mu.Unlock()
	for _, pd := range expired {
		s.failJob(pd.job, "dispatch: no ACK from worker", s.log.With("job_id", pd.job.JobID))
	}
	for _, p := range bad {
		p.conn.Close()
	}
}

func (s *Server) failJob(job judge.Job, cause string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.repo.Fail(ctx, job, cause, s.cfg.Retry); err != nil &&
		!errors.Is(err, judge.ErrLeaseLost) && !errors.Is(err, judge.ErrStale) {
		log.Error("requeue job", "cause", cause, "err", err)
		return
	}
	log.Warn("job returned to queue", "cause", cause)
}

func (p *peer) send(t wire.Type, v any) error { return writeFrame(p.conn, &p.wmu, t, v) }

// writeFrame writes one frame under mu (when given) with a write deadline,
// so a stuck peer cannot block the sender forever.
func writeFrame(conn net.Conn, mu *sync.Mutex, t wire.Type, v any) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return wire.Write(conn, t, v)
}

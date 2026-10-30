// Package dispatch pushes judge jobs from the API to workers over a
// hand-written TCP protocol (framing in internal/wire, spec in
// docs/net-protocols.md §1).
//
// The database stays the source of truth: the dispatcher claims a job in
// PostgreSQL on behalf of a connected worker (same lease as polling) and
// sends the snapshot over TCP; the worker renews the lease in the database
// while judging, and the dispatcher writes the result it receives. If the
// connection breaks, the worker writes its result to the database itself
// and goes back to polling; a job nobody finishes is recovered when its
// lease expires, exactly as before.
package dispatch

import "github.com/hoangdaulan/lan-lab-backend/internal/judge"

// ProtocolVersion is sent in HELLO; the API refuses other versions.
const ProtocolVersion = 1

type helloMsg struct {
	WorkerID string `json:"worker_id"`
	Slots    int    `json:"slots"`
	Version  int    `json:"version"`
	// Token must equal the API's DISPATCH_TOKEN when one is set, so a
	// machine on the LAN cannot pose as a worker and receive source code.
	Token string `json:"token,omitempty"`
}

type welcomeMsg struct {
	Server      string `json:"server"`
	HeartbeatMs int64  `json:"heartbeat_ms"`
}

type ackMsg struct {
	JobID    int64  `json:"job_id"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

type resultMsg struct {
	JobID   int64          `json:"job_id"`
	Outcome *judge.Outcome `json:"outcome,omitempty"`
	// Error is an infrastructure failure (retried); a program's own
	// failure (CE/WA/...) is an Outcome.
	Error   string `json:"error,omitempty"`
	JudgeMs int64  `json:"judge_ms"`
}

// Result statuses in RESULT_OK.
const (
	statusSaved   = "saved"   // written to the database
	statusDropped = "dropped" // lease lost or problem version changed: discarded on purpose
	statusUnknown = "unknown" // not dispatched on this connection: worker writes it itself
	statusError   = "error"   // database write failed: worker writes it itself
)

type resultOKMsg struct {
	JobID  int64  `json:"job_id"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type errorMsg struct {
	Message string `json:"message"`
}

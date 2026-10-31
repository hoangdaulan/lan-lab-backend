// Package wire is the framing of the hand-written TCP protocol between the
// API (job dispatcher) and judge workers (docs/net-protocols.md §1).
//
// Every message is one frame:
//
//	+----------------+------------+------------------------+
//	| length: uint32 | type: byte | payload: JSON          |
//	| big-endian     |            | (length-1 bytes)       |
//	+----------------+------------+------------------------+
//
// length counts the type byte plus the payload, so an empty payload has
// length 1. TCP is a byte stream: the length prefix is what tells the
// reader where one message ends and the next begins.
package wire

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxFrame caps one frame (type + payload). A compile log is at most a few
// hundred KB, so 4 MiB leaves room while stopping a bad length prefix from
// allocating gigabytes.
const MaxFrame = 4 << 20

// Type identifies a message.
type Type byte

const (
	Hello    Type = 0x01 // worker → API: first message {worker_id, slots, version}
	Welcome  Type = 0x02 // API → worker: handshake accepted {server, heartbeat_ms}
	JobMsg   Type = 0x03 // API → worker: a job claimed for this worker
	Ack      Type = 0x04 // worker → API: job accepted (or refused) {job_id, accepted}
	Result   Type = 0x05 // worker → API: judging finished {job_id, outcome | error}
	ResultOK Type = 0x06 // API → worker: result handled {job_id, status}
	Ping     Type = 0x07 // either side: liveness probe
	Pong     Type = 0x08 // reply to Ping
	Error    Type = 0x09 // API → worker: fatal error, connection closes next {message}
)

func (t Type) String() string {
	switch t {
	case Hello:
		return "HELLO"
	case Welcome:
		return "WELCOME"
	case JobMsg:
		return "JOB"
	case Ack:
		return "ACK"
	case Result:
		return "RESULT"
	case ResultOK:
		return "RESULT_OK"
	case Ping:
		return "PING"
	case Pong:
		return "PONG"
	case Error:
		return "ERROR"
	}
	return fmt.Sprintf("0x%02x", byte(t))
}

var (
	ErrFrameTooLarge = errors.New("wire: frame too large")
	ErrEmptyFrame    = errors.New("wire: empty frame")
)

// Write sends one frame with v encoded as JSON (nil sends no payload). The
// header and payload go out in a single Write so concurrent writers that
// serialize on a mutex never interleave partial frames.
func Write(w io.Writer, t Type, v any) error {
	var payload []byte
	if v != nil {
		var err error
		if payload, err = json.Marshal(v); err != nil {
			return err
		}
	}
	if len(payload)+1 > MaxFrame {
		return ErrFrameTooLarge
	}
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(len(payload)+1))
	buf[4] = byte(t)
	copy(buf[5:], payload)
	_, err := w.Write(buf)
	return err
}

// Read reads one frame. It returns io.EOF only on a clean close between
// frames; a close inside a frame is io.ErrUnexpectedEOF.
func Read(r io.Reader) (Type, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return 0, nil, ErrEmptyFrame
	}
	if n > MaxFrame {
		return 0, nil, ErrFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}
	return Type(body[0]), body[1:], nil
}

// Decode unmarshals a payload, rejecting an empty one.
func Decode(payload []byte, v any) error {
	if len(payload) == 0 {
		return errors.New("wire: missing payload")
	}
	return json.Unmarshal(payload, v)
}

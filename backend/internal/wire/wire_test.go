package wire

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	type msg struct {
		ID   int64  `json:"id"`
		Text string `json:"text"`
	}
	if err := Write(&buf, Ack, msg{ID: 7, Text: "xin chào"}); err != nil {
		t.Fatal(err)
	}
	if err := Write(&buf, Ping, nil); err != nil {
		t.Fatal(err)
	}
	// Header: length = 1 (type) + len(JSON).
	if got := buf.Bytes()[4]; got != byte(Ack) {
		t.Fatalf("type byte = %x", got)
	}

	typ, p, err := Read(&buf)
	if err != nil || typ != Ack {
		t.Fatalf("Read = %v, %v", typ, err)
	}
	var m msg
	if err := Decode(p, &m); err != nil || m.ID != 7 || m.Text != "xin chào" {
		t.Fatalf("Decode = %+v, %v", m, err)
	}
	typ, p, err = Read(&buf)
	if err != nil || typ != Ping || len(p) != 0 {
		t.Fatalf("Read ping = %v %q %v", typ, p, err)
	}
	if _, _, err := Read(&buf); err != io.EOF {
		t.Fatalf("clean end = %v, want EOF", err)
	}
}

func TestReadRejectsBadFrames(t *testing.T) {
	cases := map[string]struct {
		in   []byte
		want error
	}{
		"empty":     {[]byte{0, 0, 0, 0}, ErrEmptyFrame},
		"too large": {[]byte{0x7f, 0, 0, 0}, ErrFrameTooLarge},
		"truncated": {[]byte{0, 0, 0, 5, byte(Ack), '{'}, io.ErrUnexpectedEOF},
		"half hdr":  {[]byte{0, 0}, io.ErrUnexpectedEOF},
	}
	for name, c := range cases {
		if _, _, err := Read(bytes.NewReader(c.in)); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

// A reader that returns one byte at a time still yields whole frames
// (TCP may split a frame across many segments).
func TestReadAcrossShortReads(t *testing.T) {
	var buf bytes.Buffer
	_ = Write(&buf, Result, map[string]int{"job_id": 3})
	typ, p, err := Read(&oneByte{buf.Bytes()})
	if err != nil || typ != Result || string(p) != `{"job_id":3}` {
		t.Fatalf("got %v %q %v", typ, p, err)
	}
}

type oneByte struct{ b []byte }

func (r *oneByte) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	p[0] = r.b[0]
	r.b = r.b[1:]
	return 1, nil
}

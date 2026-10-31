// Package mcast mirrors teacher announcements to a UDP multicast group
// (docs/net-protocols.md §3), alongside the WebSocket push.
//
// One datagram reaches every machine in the lab that joined the group
// (IGMP), however many there are: the server sends once instead of once per
// connection. The default TTL of 1 keeps datagrams inside the local subnet.
// Delivery is best effort: Seq lets a receiver notice gaps, and the
// WebSocket/REST path stays the reliable source.
package mcast

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

// Magic tags every datagram (shared with internal/discovery).
const Magic = "LANLAB/1"

// DefaultGroup is an address in the organization-local scope (239/8).
const DefaultGroup = "239.255.42.99:9998"

const maxDatagram = 8 << 10

// Announcement is one multicast datagram.
type Announcement struct {
	Magic     string    `json:"magic"`
	Type      string    `json:"type"` // "announcement"
	Server    string    `json:"server,omitempty"`
	Seq       uint64    `json:"seq"`
	ID        int64     `json:"id"`
	SessionID int64     `json:"session_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// Sender writes announcements to a multicast group.
type Sender struct {
	conn   *net.UDPConn
	server string
	seq    atomic.Uint64
}

// NewSender prepares sending to group ("239.255.42.99:9998"). The kernel
// picks the outgoing interface from the routing table.
func NewSender(group, server string) (*Sender, error) {
	ga, err := resolveGroup(group)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp4", nil, ga)
	if err != nil {
		return nil, err
	}
	return &Sender{conn: conn, server: server}, nil
}

// Send multicasts one announcement.
func (s *Sender) Send(id, sessionID int64, text string, createdAt time.Time) error {
	b, err := json.Marshal(Announcement{Magic: Magic, Type: "announcement", Server: s.server,
		Seq: s.seq.Add(1), ID: id, SessionID: sessionID, Text: text, CreatedAt: createdAt})
	if err != nil {
		return err
	}
	if len(b) > maxDatagram {
		return errors.New("mcast: announcement too large")
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = s.conn.Write(b)
	return err
}

func (s *Sender) Close() error { return s.conn.Close() }

// Listen joins group on iface (nil: the system default) and calls fn for
// each announcement until ctx ends.
func Listen(ctx context.Context, group string, iface *net.Interface, fn func(Announcement, *net.UDPAddr)) error {
	ga, err := resolveGroup(group)
	if err != nil {
		return err
	}
	conn, err := net.ListenMulticastUDP("udp4", iface, ga)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	buf := make([]byte, maxDatagram)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		var a Announcement
		if json.Unmarshal(buf[:n], &a) == nil && a.Magic == Magic && a.Type == "announcement" {
			fn(a, src)
		}
	}
}

func resolveGroup(group string) (*net.UDPAddr, error) {
	ga, err := net.ResolveUDPAddr("udp4", group)
	if err != nil {
		return nil, err
	}
	if !ga.IP.IsMulticast() {
		return nil, errors.New("mcast: " + group + " is not a multicast address (224.0.0.0/4)")
	}
	return ga, nil
}

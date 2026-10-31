// Package discovery lets machines on the LAN find the server without
// typing its IP, over UDP broadcast (docs/net-protocols.md §2).
//
// The API binds UDP DISCOVERY_PORT and
//   - every interval broadcasts an "announce" datagram to the directed
//     broadcast address of each IPv4 interface (e.g. 192.168.1.255), and
//   - answers a "discover" datagram (sent by a client to the broadcast
//     address) with a unicast "announce" back to the sender.
//
// Datagrams are single JSON objects tagged with Magic so unrelated traffic
// on the port is ignored. UDP may drop or duplicate datagrams; clients just
// keep the latest announcement per server.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync/atomic"
	"time"
)

// Magic identifies this protocol in every datagram.
const Magic = "LANLAB/1"

// DefaultPort is the UDP port used when none is configured.
const DefaultPort = 9999

const (
	TypeAnnounce = "announce"
	TypeDiscover = "discover"
)

// maxDatagram bounds reads; announcements are a few hundred bytes.
const maxDatagram = 2048

// Message is one discovery datagram.
type Message struct {
	Magic string `json:"magic"`
	Type  string `json:"type"`
	Name  string `json:"name,omitempty"`
	// URLs and addresses this server serves, as reachable from the
	// receiver's subnet.
	HTTP      string `json:"http,omitempty"`
	HTTPS     string `json:"https,omitempty"`
	Dispatch  string `json:"dispatch,omitempty"`  // judge worker TCP host:port
	Multicast string `json:"multicast,omitempty"` // announcement group host:port
	Seq       uint64 `json:"seq,omitempty"`
}

// Parse decodes a datagram, rejecting anything without the magic tag.
func Parse(b []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return Message{}, err
	}
	if m.Magic != Magic {
		return Message{}, errors.New("not a " + Magic + " datagram")
	}
	return m, nil
}

// Service describes what the server announces. Ports are the ports as
// seen from the LAN; an empty port leaves that field out.
type Service struct {
	Name         string
	Host         string // fixed host to advertise; empty: the IP of each interface
	HTTPPort     string
	HTTPSPort    string
	DispatchPort string
	Multicast    string // full group address, advertised as is
}

func (s Service) message(host string, seq uint64) Message {
	if s.Host != "" {
		host = s.Host
	}
	m := Message{Magic: Magic, Type: TypeAnnounce, Name: s.Name, Multicast: s.Multicast, Seq: seq}
	if s.HTTPPort != "" {
		m.HTTP = "http://" + net.JoinHostPort(host, s.HTTPPort)
	}
	if s.HTTPSPort != "" {
		m.HTTPS = "https://" + net.JoinHostPort(host, s.HTTPSPort)
	}
	if s.DispatchPort != "" {
		m.Dispatch = net.JoinHostPort(host, s.DispatchPort)
	}
	return m
}

// Announcer broadcasts the service and answers probes.
type Announcer struct {
	Service  Service
	Port     int
	Interval time.Duration
	Log      *slog.Logger
	seq      atomic.Uint64
}

// Run serves until ctx ends.
func (a *Announcer) Run(ctx context.Context) error {
	conn, err := listenShared(ctx, a.Port)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	a.Log.Info("discovery announcer started", "port", a.Port, "interval", a.Interval)

	go a.broadcastLoop(ctx, conn)
	buf := make([]byte, maxDatagram)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		m, err := Parse(buf[:n])
		if err != nil || m.Type != TypeDiscover {
			continue // our own broadcasts, other servers, noise
		}
		reply := a.Service.message(localIPFor(src.IP), a.seq.Add(1))
		if b, err := json.Marshal(reply); err == nil {
			_, _ = conn.WriteToUDP(b, src)
		}
		a.Log.Debug("answered discovery probe", "from", src.String())
	}
}

func (a *Announcer) broadcastLoop(ctx context.Context, conn *net.UDPConn) {
	t := time.NewTicker(a.Interval)
	defer t.Stop()
	for {
		for _, iface := range broadcastInterfaces() {
			b, err := json.Marshal(a.Service.message(iface.IP.String(), a.seq.Add(1)))
			if err != nil {
				continue
			}
			dst := &net.UDPAddr{IP: iface.Broadcast, Port: a.Port}
			if _, err := conn.WriteToUDP(b, dst); err != nil && ctx.Err() == nil {
				a.Log.Debug("broadcast failed", "dst", dst.String(), "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Found is an announcement and where it came from.
type Found struct {
	Message
	From *net.UDPAddr
}

// Probe broadcasts a "discover" datagram on port and collects the answers
// received within wait (one per distinct server). Extra targets (e.g.
// "127.0.0.1:9999") are probed too.
func Probe(ctx context.Context, port int, wait time.Duration, extra ...string) ([]Found, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	probe, _ := json.Marshal(Message{Magic: Magic, Type: TypeDiscover})

	targets := []*net.UDPAddr{{IP: net.IPv4bcast, Port: port}}
	for _, iface := range broadcastInterfaces() {
		targets = append(targets, &net.UDPAddr{IP: iface.Broadcast, Port: port})
	}
	for _, e := range extra {
		if ua, err := net.ResolveUDPAddr("udp4", e); err == nil {
			targets = append(targets, ua)
		}
	}
	sent := 0
	for _, t := range targets {
		if _, err := conn.WriteToUDP(probe, t); err == nil {
			sent++
		}
	}
	if sent == 0 {
		return nil, errors.New("discovery: could not send any probe")
	}

	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()

	seen := map[string]bool{}
	var out []Found
	buf := make([]byte, maxDatagram)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return out, nil
			}
			return out, err
		}
		m, err := Parse(buf[:n])
		if err != nil || m.Type != TypeAnnounce {
			continue
		}
		key := m.Name + "|" + m.HTTP + "|" + m.Dispatch
		if !seen[key] {
			seen[key] = true
			out = append(out, Found{Message: m, From: src})
		}
	}
}

// Watch listens passively for periodic announcements on port and calls fn
// for each, until ctx ends.
func Watch(ctx context.Context, port int, fn func(Found)) error {
	conn, err := listenShared(ctx, port)
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
		if m, err := Parse(buf[:n]); err == nil && m.Type == TypeAnnounce {
			fn(Found{Message: m, From: src})
		}
	}
}

// FindDispatcher probes until a server advertising a dispatch address
// answers, retrying every few seconds until ctx ends.
func FindDispatcher(ctx context.Context, port int) (string, error) {
	for {
		found, err := Probe(ctx, port, 2*time.Second)
		for _, f := range found {
			if f.Dispatch != "" {
				return f.Dispatch, nil
			}
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err == nil {
			err = errors.New("no server answered")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// listenShared binds UDP port on all IPv4 interfaces with SO_REUSEADDR, so
// the server and a watching client can share the port on one machine and
// every socket gets a copy of each broadcast.
func listenShared(ctx context.Context, port int) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: reuseAddr}
	pc, err := lc.ListenPacket(ctx, "udp4", ":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// ifaceAddr is an IPv4 address of an up, broadcast-capable interface.
type ifaceAddr struct {
	IP        net.IP
	Broadcast net.IP
}

func broadcastInterfaces() []ifaceAddr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ifaceAddr
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if b := BroadcastAddr(ipn); b != nil {
					out = append(out, ifaceAddr{IP: ipn.IP.To4(), Broadcast: b})
				}
			}
		}
	}
	return out
}

// BroadcastAddr is the directed broadcast address of an IPv4 network
// (host bits all ones), or nil for IPv6 and /31-/32 networks.
func BroadcastAddr(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	if ip == nil || len(n.Mask) != net.IPv4len {
		return nil
	}
	if ones, _ := n.Mask.Size(); ones > 30 {
		return nil
	}
	b := make(net.IP, net.IPv4len)
	for i := range ip {
		b[i] = ip[i] | ^n.Mask[i]
	}
	return b
}

// localIPFor picks this machine's address on the subnet of remote, so a
// probe answer names an IP the prober can reach.
func localIPFor(remote net.IP) string {
	if remote.IsLoopback() {
		return remote.String()
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && ipn.Contains(remote) {
				return ipn.IP.String()
			}
		}
	}
	// Different subnet: let the kernel pick the outgoing address.
	if c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: remote, Port: 9}); err == nil {
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).IP.String()
	}
	return ""
}

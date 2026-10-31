package discovery

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestBroadcastAddr(t *testing.T) {
	cases := map[string]string{
		"192.168.1.9/24": "192.168.1.255",
		"10.0.5.20/16":   "10.0.255.255",
		"172.16.3.1/30":  "172.16.3.3",
		"10.0.0.1/32":    "<nil>",
		"fe80::1/64":     "<nil>",
	}
	for cidr, want := range cases {
		ip, n, _ := net.ParseCIDR(cidr)
		n.IP = ip
		if got := BroadcastAddr(n).String(); got != want {
			t.Errorf("%s: %s, want %s", cidr, got, want)
		}
	}
}

func TestParseRejectsForeignDatagrams(t *testing.T) {
	for _, in := range []string{`hello`, `{"type":"announce"}`, `{"magic":"OTHER","type":"announce"}`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%s) accepted", in)
		}
	}
}

// A probe (sent to loopback, since loopback has no broadcast) gets a
// unicast answer describing the service.
func TestProbeGetsAnswer(t *testing.T) {
	port := freeUDPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &Announcer{
		Service:  Service{Name: "lab", HTTPPort: "8080", DispatchPort: "7070"},
		Port:     port,
		Interval: time.Hour,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go func() { _ = a.Run(ctx) }()

	var found []Found
	for i := 0; i < 20 && len(found) == 0; i++ {
		var err error
		found, err = Probe(ctx, port, 100*time.Millisecond, "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(found) == 0 {
		t.Fatal("no answer")
	}
	f := found[0]
	if f.Name != "lab" || f.HTTP != "http://127.0.0.1:8080" || f.Dispatch != "127.0.0.1:7070" {
		t.Fatalf("answer = %+v", f.Message)
	}
}

func freeUDPPort(t *testing.T) int {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

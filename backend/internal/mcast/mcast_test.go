package mcast

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRejectsUnicastGroup(t *testing.T) {
	if _, err := NewSender("192.168.1.9:9998", "x"); err == nil {
		t.Fatal("unicast address accepted as group")
	}
}

// Sends to the group and receives through multicast loopback on this host.
// Skipped where the sandbox has no multicast route.
func TestSendReceive(t *testing.T) {
	group := "239.255.42.99:19998"
	s, err := NewSender(group, "lab")
	if err != nil {
		t.Skip("no multicast route:", err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan Announcement, 1)
	go func() {
		_ = Listen(ctx, group, nil, func(a Announcement, _ *net.UDPAddr) {
			select {
			case got <- a:
			default:
			}
		})
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := s.Send(5, 3, "Còn 10 phút", time.Now()); err != nil {
			t.Skip("multicast send failed:", err)
		}
		select {
		case a := <-got:
			if a.ID != 5 || a.SessionID != 3 || a.Text != "Còn 10 phút" || a.Server != "lab" || a.Seq == 0 {
				t.Fatalf("got %+v", a)
			}
			return
		case <-ctx.Done():
			t.Skip("no multicast loopback in this environment")
		case <-tick.C:
		}
	}
}

// Command lannet is a small client for the server's UDP features, for
// students' machines and for the demo (docs/net-protocols.md):
//
//	lannet discover [-port 9999] [-wait 2s] [-to host:port]
//	    broadcast a probe and print every server that answers
//	lannet watch [-port 9999]
//	    print the server's periodic broadcast announcements
//	lannet announcements [-group 239.255.42.99:9998] [-iface wlan0]
//	    join the multicast group and print teacher announcements
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/discovery"
	"github.com/hoangdaulan/lan-lab-backend/internal/mcast"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var err error
	switch os.Args[1] {
	case "discover":
		err = discover(ctx, os.Args[2:])
	case "watch":
		err = watch(ctx, os.Args[2:])
	case "announcements":
		err = announcements(ctx, os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lannet:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: lannet discover|watch|announcements [flags]")
	os.Exit(2)
}

func discover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	port := fs.Int("port", discovery.DefaultPort, "discovery UDP port")
	wait := fs.Duration("wait", 2*time.Second, "how long to collect answers")
	to := fs.String("to", "", "also probe this host:port directly (comma-separated)")
	_ = fs.Parse(args)

	var extra []string
	for _, t := range strings.Split(*to, ",") {
		if t = strings.TrimSpace(t); t != "" {
			extra = append(extra, t)
		}
	}
	found, err := discovery.Probe(ctx, *port, *wait, extra...)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return fmt.Errorf("no server answered on UDP port %d within %s", *port, *wait)
	}
	for _, f := range found {
		printServer(f)
	}
	return nil
}

func watch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	port := fs.Int("port", discovery.DefaultPort, "discovery UDP port")
	_ = fs.Parse(args)
	fmt.Printf("listening for announcements on UDP :%d (Ctrl+C to stop)\n", *port)
	return discovery.Watch(ctx, *port, printServer)
}

func announcements(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("announcements", flag.ExitOnError)
	group := fs.String("group", mcast.DefaultGroup, "multicast group host:port")
	ifName := fs.String("iface", "", "network interface to join on (default: system choice)")
	_ = fs.Parse(args)
	var ifi *net.Interface
	if *ifName != "" {
		var err error
		if ifi, err = net.InterfaceByName(*ifName); err != nil {
			return err
		}
	}
	fmt.Printf("joined multicast group %s (Ctrl+C to stop)\n", *group)
	var last uint64
	return mcast.Listen(ctx, *group, ifi, func(a mcast.Announcement, src *net.UDPAddr) {
		gap := ""
		if last != 0 && a.Seq > last+1 {
			gap = fmt.Sprintf("  (missed %d datagram(s))", a.Seq-last-1)
		}
		last = a.Seq
		fmt.Printf("[%s] session %d from %s #%d: %s%s\n",
			a.CreatedAt.Local().Format("15:04:05"), a.SessionID, src.IP, a.Seq, a.Text, gap)
	})
}

func printServer(f discovery.Found) {
	fmt.Printf("%s (from %s)\n", f.Name, f.From)
	for _, kv := range [][2]string{{"http", f.HTTP}, {"https", f.HTTPS}, {"dispatch", f.Dispatch}, {"multicast", f.Multicast}} {
		if kv[1] != "" {
			fmt.Printf("  %-9s %s\n", kv[0], kv[1])
		}
	}
}

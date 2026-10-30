package tlscert

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"
)

func TestEnsureCreatesReusesAndRenews(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	hosts := []string{"localhost", "127.0.0.1", "192.168.1.9"}

	certFile, keyFile, created, err := Ensure(dir, hosts, nil, now)
	if err != nil || !created {
		t.Fatalf("first Ensure: created=%v err=%v", created, err)
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(pair.Certificate[0])
	for _, h := range hosts {
		if err := cert.VerifyHostname(h); err != nil {
			t.Errorf("certificate does not cover %s: %v", h, err)
		}
	}
	if fi, _ := os.Stat(keyFile); fi.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v", fi.Mode().Perm())
	}

	// Extra hosts (container addresses) never force a new certificate.
	if _, _, created, err := Ensure(dir, hosts[:2], []string{"172.24.0.9"}, now.Add(24*time.Hour)); err != nil || created {
		t.Errorf("subset of hosts should reuse: created=%v err=%v", created, err)
	}
	if _, _, created, _ := Ensure(dir, append(hosts, "10.0.0.5"), nil, now); !created {
		t.Error("new host should regenerate")
	}
	if _, _, created, _ := Ensure(dir, hosts, nil, now.Add(validFor-renewAt+time.Hour)); !created {
		t.Error("near expiry should regenerate")
	}
}

func TestRequiredHostsAddsConfiguredOnce(t *testing.T) {
	h := RequiredHosts([]string{"192.168.1.9", "localhost", "", "judge.lan"})
	want := []string{"localhost", "127.0.0.1", "192.168.1.9", "judge.lan"}
	if len(h) != len(want) {
		t.Fatalf("hosts = %v", h)
	}
	for i := range want {
		if h[i] != want[i] {
			t.Errorf("hosts = %v, want %v", h, want)
		}
	}
}

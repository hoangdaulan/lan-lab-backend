// Package tlscert keeps a self-signed certificate for the API's HTTPS
// listener. Browsers only allow screen capture (getDisplayMedia) in a secure
// context, and http://<LAN-IP> is not one; a self-signed certificate is
// enough once each browser accepts the warning (or trusts the certificate).
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	certName = "cert.pem"
	keyName  = "key.pem"
	validFor = 2 * 365 * 24 * time.Hour
	renewAt  = 30 * 24 * time.Hour // regenerate when this close to expiry
)

// Ensure returns the certificate and key files in dir, creating them when
// missing, expiring, or not covering every host in required (DNS names or
// IPs). A new certificate also lists extra (e.g. interface addresses, which
// change between container restarts, so they never force a new one: every
// new certificate means another browser warning).
func Ensure(dir string, required, extra []string, now time.Time) (certFile, keyFile string, created bool, err error) {
	certFile, keyFile = filepath.Join(dir, certName), filepath.Join(dir, keyName)
	if ok, err := covers(certFile, required, now); err != nil {
		return "", "", false, err
	} else if ok {
		return certFile, keyFile, false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", false, fmt.Errorf("create %s: %w", dir, err)
	}
	hosts := slices.Clone(required)
	for _, h := range extra {
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	certPEM, keyPEM, err := generate(hosts, now)
	if err != nil {
		return "", "", false, err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return "", "", false, err
	}
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return "", "", false, err
	}
	return certFile, keyFile, true, nil
}

// covers reports whether the existing certificate is valid for every host
// until well after now.
func covers(certFile string, hosts []string, now time.Time) (bool, error) {
	b, err := os.ReadFile(certFile)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return false, nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, nil
	}
	if now.Add(renewAt).After(cert.NotAfter) {
		return false, nil
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			if !slices.ContainsFunc(cert.IPAddresses, ip.Equal) {
				return false, nil
			}
		} else if !slices.Contains(cert.DNSNames, h) {
			return false, nil
		}
	}
	return true, nil
}

func generate(hosts []string, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LAN Judge", Organization: []string{"LAN Judge (self-signed)"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // lets a browser/OS trust it directly as its own root
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

// RequiredHosts is what the certificate must always cover: localhost and
// the configured names/IPs (the server's LAN address).
func RequiredHosts(configured []string) []string {
	hosts := []string{"localhost", "127.0.0.1"}
	for _, h := range configured {
		if h != "" && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// InterfaceIPs lists this machine's non-loopback IPv4 addresses (useful when
// the API runs directly on the server rather than in a container).
func InterfaceIPs() []string {
	var out []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				out = append(out, n.IP.String())
			}
		}
	}
	return out
}

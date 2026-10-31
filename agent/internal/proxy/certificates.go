package proxy

import (
	"crypto/x509"
	"encoding/pem"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const maxCertificateBytes = 1 << 20

// certificates reports one entry for every hostname the managed proxy serves:
// valid or failed when Caddy stored a certificate, pending while it has none.
func (m *Manager) certificates(hostnames []string) []*agentv1.ProxyCertificate {
	found := m.storedCertificates()
	now := time.Now()
	result := make([]*agentv1.ProxyCertificate, 0, len(hostnames))
	for _, hostname := range hostnames {
		expires, ok := found[hostname]
		if !ok {
			result = append(result, &agentv1.ProxyCertificate{Hostname: hostname, State: "pending"})
			continue
		}
		state := "valid"
		if !expires.After(now) {
			state = "failed"
		}
		result = append(result, &agentv1.ProxyCertificate{
			Hostname:  hostname,
			State:     state,
			ExpiresAt: timestamppb.New(expires),
		})
	}
	return result
}

// storedCertificates reads the leaf certificates Caddy keeps under its data
// directory. An unreadable certificate is simply not reported.
func (m *Manager) storedCertificates() map[string]time.Time {
	root := filepath.Join(m.root, "data", "caddy", "certificates")
	expiry := map[string]time.Time{}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".crt") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxCertificateBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, cert := range parseCertificates(data) {
			names := append([]string(nil), cert.DNSNames...)
			if cert.Subject.CommonName != "" {
				names = append(names, cert.Subject.CommonName)
			}
			for _, name := range names {
				name = strings.ToLower(strings.TrimSpace(name))
				if !validHostname(name) {
					continue
				}
				if previous, ok := expiry[name]; !ok || cert.NotAfter.After(previous) {
					expiry[name] = cert.NotAfter
				}
			}
		}
		return nil
	})
	return expiry
}

func parseCertificates(data []byte) []*x509.Certificate {
	var certificates []*x509.Certificate
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		certificates = append(certificates, cert)
	}
	return certificates
}

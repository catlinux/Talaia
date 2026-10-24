package notify

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type smtpRecord struct {
	command string
	tls     bool
}

type fakeSMTP struct {
	listener net.Listener
	cert     tls.Certificate
	startTLS bool
	implicit bool
	mu       sync.Mutex
	records  []smtpRecord
	data     string
	commands []string
	closed   chan struct{}
}

func newFakeSMTP(t *testing.T, startTLS, implicit bool) (*fakeSMTP, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ip := netip.MustParseAddr("127.0.0.1").AsSlice()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{ip}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{listener: ln, cert: cert, startTLS: startTLS, implicit: implicit, closed: make(chan struct{})}
	go f.serve()
	t.Cleanup(func() { ln.Close(); <-f.closed })
	return f, pool
}

func (f *fakeSMTP) add(command string, secure bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, smtpRecord{command, secure})
	f.commands = append(f.commands, command)
}

func (f *fakeSMTP) serve() {
	defer close(f.closed)
	raw, err := f.listener.Accept()
	if err != nil {
		return
	}
	defer raw.Close()
	conn := net.Conn(raw)
	secure := false
	if f.implicit {
		tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{f.cert}})
		if tc.Handshake() != nil {
			return
		}
		conn = tc
		secure = true
	}
	write := func(s string) bool { _, e := conn.Write([]byte(s + "\r\n")); return e == nil }
	if !write("220 fake smtp") {
		return
	}
	reader := bufio.NewReader(conn)
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		f.add(cmd, secure)
		switch {
		case strings.HasPrefix(cmd, "EHLO "):
			if f.startTLS && !secure {
				if !write("250-fake smtp") || !write("250-STARTTLS") || !write("250 AUTH PLAIN") {
					return
				}
			} else if !write("250 fake smtp") {
				return
			}
		case cmd == "STARTTLS":
			if !f.startTLS || secure {
				write("502 STARTTLS unavailable")
				continue
			}
			if !write("220 ready") {
				return
			}
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{f.cert}})
			if tc.Handshake() != nil {
				return
			}
			conn = tc
			reader = bufio.NewReader(conn)
			secure = true
		case strings.HasPrefix(cmd, "AUTH PLAIN "):
			if !write("235 authenticated") {
				return
			}
		case strings.HasPrefix(cmd, "MAIL FROM:") || strings.HasPrefix(cmd, "RCPT TO:"):
			if !write("250 accepted") {
				return
			}
		case cmd == "DATA":
			if !write("354 send data") {
				return
			}
			var body strings.Builder
			for {
				line, e = reader.ReadString('\n')
				if e != nil {
					return
				}
				if strings.TrimRight(line, "\r\n") == "." {
					break
				}
				body.WriteString(line)
			}
			f.mu.Lock()
			f.data = body.String()
			f.mu.Unlock()
			if !write("250 queued") {
				return
			}
		case cmd == "QUIT":
			write("221 bye")
			return
		default:
			write("500 unsupported")
		}
	}
}

func (f *fakeSMTP) snapshot() ([]smtpRecord, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]smtpRecord(nil), f.records...), f.data
}

func TestEmailSTARTTLSAndEncodedUTF8Message(t *testing.T) {
	f, roots := newFakeSMTP(t, true, false)
	e := Email{Host: "127.0.0.1", Port: f.listener.Addr().(*net.TCPAddr).Port, From: "sender@example.com", To: []string{"one@example.com", "two@example.com"}, Username: "user-secret", Password: "pass-secret", TLSMode: "starttls", AllowPrivate: func() bool { return true }, RootCAs: &tls.Config{RootCAs: roots}}
	if err := e.Send(context.Background(), Message{Title: "Alerta d'ús", Body: "cos amb accent: à"}); err != nil {
		t.Fatal(err)
	}
	records, body := f.snapshot()
	seen := map[string]bool{}
	for _, rec := range records {
		if strings.HasPrefix(rec.command, "AUTH PLAIN ") || strings.HasPrefix(rec.command, "MAIL FROM:") || strings.HasPrefix(rec.command, "RCPT TO:") || rec.command == "DATA" {
			if !rec.tls {
				t.Errorf("sensitive command before TLS: %+v", rec)
			}
		}
		if rec.command == "STARTTLS" {
			seen["STARTTLS"] = true
		}
		if strings.HasPrefix(rec.command, "AUTH PLAIN ") {
			seen["AUTH"] = true
		}
		if strings.HasPrefix(rec.command, "MAIL FROM:") {
			seen["MAIL"] = true
		}
		if strings.HasPrefix(rec.command, "RCPT TO:") {
			seen[rec.command] = true
		}
		if rec.command == "DATA" {
			seen["DATA"] = true
		}
	}
	for _, key := range []string{"STARTTLS", "AUTH", "MAIL", "DATA", "RCPT TO:<one@example.com>", "RCPT TO:<two@example.com>"} {
		if !seen[key] {
			t.Errorf("missing command %q", key)
		}
	}
	if !strings.Contains(body, "Subject: =?utf-8?q?") || !strings.Contains(body, "To: one@example.com, two@example.com") || !strings.Contains(body, "cos amb accent: à") {
		t.Errorf("unexpected message: %q", body)
	}
}

func TestEmailSTARTTLSRequiredBeforeSecrets(t *testing.T) {
	f, roots := newFakeSMTP(t, false, false)
	e := Email{Host: "127.0.0.1", Port: f.listener.Addr().(*net.TCPAddr).Port, From: "sender@example.com", To: []string{"to@example.com"}, Username: "user-secret", Password: "pass-secret", TLSMode: "starttls", AllowPrivate: func() bool { return true }, RootCAs: &tls.Config{RootCAs: roots}}
	if err := e.Send(context.Background(), Message{Body: "private body"}); err == nil {
		t.Fatal("Send succeeded without STARTTLS")
	}
	records, _ := f.snapshot()
	for _, r := range records {
		if strings.HasPrefix(r.command, "AUTH ") || strings.HasPrefix(r.command, "MAIL ") || r.command == "DATA" {
			t.Errorf("sensitive command received: %q", r.command)
		}
	}
}

func TestEmailImplicitTLS(t *testing.T) {
	f, roots := newFakeSMTP(t, false, true)
	e := Email{Host: "127.0.0.1", Port: f.listener.Addr().(*net.TCPAddr).Port, From: "sender@example.com", To: []string{"to@example.com"}, TLSMode: "tls", AllowPrivate: func() bool { return true }, RootCAs: &tls.Config{RootCAs: roots}}
	if err := e.Send(context.Background(), Message{Title: "ok", Body: "hello"}); err != nil {
		records, _ := f.snapshot()
		t.Fatalf("Send error=%v records=%+v", err, records)
	}
	records, _ := f.snapshot()
	for _, r := range records {
		if r.command == "MAIL FROM:<sender@example.com>" && !r.tls {
			t.Errorf("MAIL was not TLS protected: %+v", r)
		}
	}
}

func TestEmailUntrustedTLSAndErrorRedaction(t *testing.T) {
	f, _ := newFakeSMTP(t, true, false)
	e := Email{Host: "127.0.0.1", Port: f.listener.Addr().(*net.TCPAddr).Port, From: "sender@example.com", To: []string{"to@example.com"}, Username: "user-secret", Password: "pass-secret", TLSMode: "starttls", AllowPrivate: func() bool { return true }}
	err := e.Send(context.Background(), Message{Body: "private-body-secret"})
	if err == nil || err.Error() != "tls error" {
		t.Fatalf("Send error=%v", err)
	}
	records, _ := f.snapshot()
	for _, r := range records {
		if strings.HasPrefix(r.command, "AUTH ") {
			t.Fatal("AUTH received before trusted TLS")
		}
	}
	for _, secret := range []string{"user-secret", "pass-secret", "private-body-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

func TestEmailPrivateAddressBlockedBeforeConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	e := Email{Host: "127.0.0.1", Port: addr.Port, From: "sender@example.com", To: []string{"to@example.com"}, TLSMode: "tls", AllowPrivate: func() bool { return false }}
	if err := e.Send(context.Background(), Message{}); err == nil || err.Error() != "blocked address" {
		t.Fatalf("Send error=%v", err)
	}
	_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Millisecond))
	if _, err := ln.Accept(); err == nil {
		t.Fatal("connected to private listener")
	}
}

func TestEmailRejectsHeaderInjectionBeforeConnecting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		email Email
		title string
	}{
		{"subject", Email{Host: "127.0.0.1", Port: 1, From: "sender@example.com", To: []string{"to@example.com"}, TLSMode: "tls"}, "hello\r\nBcc: victim@example.com"},
		{"recipient", Email{Host: "127.0.0.1", Port: 1, From: "sender@example.com", To: []string{"to@example.com\nBcc: victim@example.com"}, TLSMode: "tls"}, "hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.email.Send(context.Background(), Message{Title: tc.title}); err == nil || !strings.Contains(err.Error(), "invalid header") {
				t.Fatalf("Send error=%v", err)
			}
		})
	}
}

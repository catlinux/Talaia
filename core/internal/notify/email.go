package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/catlinux/Talaia/core/internal/netguard"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

type Email struct {
	Host                        string
	Port                        int
	From                        string
	To                          []string
	Username, Password, TLSMode string
	AllowPrivate                func() bool
	RootCAs                     *tls.Config
}

func (s Email) Send(ctx context.Context, m Message) error {
	if badHeader(s.From) || badHeader(m.Title) {
		return errors.New("invalid header")
	}
	for _, to := range s.To {
		if badHeader(to) {
			return errors.New("invalid header")
		}
	}
	if len(s.To) == 0 {
		return errors.New("recipient required")
	}
	if s.Port < 1 || s.Port > 65535 || (s.TLSMode != "tls" && s.TLSMode != "starttls") {
		return errors.New("invalid email settings")
	}
	raw, e := netguard.NewDialer(s.AllowPrivate, 20*time.Second).DialContext(ctx, "tcp", net.JoinHostPort(s.Host, fmt.Sprint(s.Port)))
	if e != nil {
		return errorsClass(e)
	}
	defer func() { _ = raw.Close() }()
	_ = raw.SetDeadline(time.Now().Add(20 * time.Second))
	conn := raw
	if s.TLSMode == "tls" {
		// Implicit TLS: the handshake comes before any SMTP byte.
		tc := tls.Client(raw, tlsConfig(s))
		if e = tc.HandshakeContext(ctx); e != nil {
			return errors.New("tls error")
		}
		conn = tc
	}
	tp := textproto.NewConn(conn)
	if _, _, e = tp.ReadResponse(220); e != nil {
		return errorsClass(e)
	}
	if _, e = tp.Cmd("EHLO talaia.local"); e != nil {
		return errorsClass(e)
	}
	if _, _, e = tp.ReadResponse(250); e != nil {
		return errorsClass(e)
	}
	if s.TLSMode == "starttls" {
		if _, e = tp.Cmd("STARTTLS"); e != nil {
			return errorsClass(e)
		}
		if _, _, e = tp.ReadResponse(220); e != nil {
			return errors.New("starttls required")
		}
		cfg := tlsConfig(s)
		tc := tls.Client(raw, cfg)
		if e = tc.HandshakeContext(ctx); e != nil {
			return errors.New("tls error")
		}
		conn = tc
		tp = textproto.NewConn(conn)
		if _, e = tp.Cmd("EHLO talaia.local"); e != nil {
			return errorsClass(e)
		}
		if _, _, e = tp.ReadResponse(250); e != nil {
			return errorsClass(e)
		}
	}
	if s.Username != "" {
		auth := base64.StdEncoding.EncodeToString([]byte("\x00" + s.Username + "\x00" + s.Password))
		if _, e = tp.Cmd("AUTH PLAIN %s", auth); e != nil {
			return errors.New("authentication failed")
		}
		if _, _, e = tp.ReadResponse(235); e != nil {
			return errors.New("authentication failed")
		}
	}
	from, e := mail.ParseAddress(s.From)
	if e != nil {
		return errors.New("invalid sender")
	}
	if _, e = tp.Cmd("MAIL FROM:<%s>", from.Address); e != nil {
		return errorsClass(e)
	}
	if _, _, e = tp.ReadResponse(250); e != nil {
		return errors.New("mail rejected")
	}
	for _, raw := range s.To {
		// The envelope takes the bare address: "Ops <ops@example.com>" is a
		// header form that SMTP servers reject in RCPT TO.
		to, err := mail.ParseAddress(raw)
		if err != nil {
			return errors.New("invalid recipient")
		}
		if _, e = tp.Cmd("RCPT TO:<%s>", to.Address); e != nil {
			return errors.New("recipient rejected")
		}
		if _, _, e = tp.ReadResponse(250); e != nil {
			return errors.New("recipient rejected")
		}
	}
	if _, e = tp.Cmd("DATA"); e != nil {
		return errorsClass(e)
	}
	if _, _, e = tp.ReadResponse(354); e != nil {
		return errors.New("message rejected")
	}
	// DotWriter turns "\n" into CRLF, escapes lines that start with a dot and
	// writes the terminator, so the body can never end DATA early and smuggle
	// SMTP commands.
	w := tp.DotWriter()
	subject := mime.QEncoding.Encode("utf-8", m.Title)
	body := strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\r", "\n")
	if _, e = fmt.Fprintf(w, "From: %s\nTo: %s\nSubject: %s\nMIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\n%s\n", s.From, strings.Join(s.To, ", "), subject, body); e != nil {
		return errors.New("message failed")
	}
	if e = w.Close(); e != nil {
		return errors.New("message failed")
	}
	if _, _, e = tp.ReadResponse(250); e != nil {
		return errors.New("message failed")
	}
	_, _ = tp.Cmd("QUIT")
	return nil
}
func tlsConfig(s Email) *tls.Config {
	c := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	if s.RootCAs != nil {
		c.RootCAs = s.RootCAs.RootCAs
	}
	return c
}
func badHeader(s string) bool { return strings.ContainsAny(s, "\r\n") }
func errorsClass(e error) error {
	if errors.Is(e, netguard.ErrBlockedAddress) {
		return errors.New("blocked address")
	}
	var ne net.Error
	if errors.As(e, &ne) && ne.Timeout() {
		return errors.New("timeout")
	}
	return errors.New("connection failed")
}

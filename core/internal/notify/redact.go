package notify

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/netguard"
)

func Redact(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "[redacted]")
		}
	}
	return msg
}

func safeHTTPError(err error, status int) error {
	if status != 0 {
		return errors.New("http status " + http.StatusText(status))
	}
	if errors.Is(err, netguard.ErrBlockedAddress) {
		return errors.New("blocked address")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return errors.New("timeout")
	}
	if strings.Contains(strings.ToLower(err.Error()), "tls") || strings.Contains(strings.ToLower(err.Error()), "certificate") {
		return errors.New("tls error")
	}
	return errors.New("connection failed")
}

func client(allow func() bool) *http.Client {
	return netguard.NewHTTPClient(netguard.HTTPClientOptions{AllowPrivate: allow, Timeout: 10 * time.Second})
}

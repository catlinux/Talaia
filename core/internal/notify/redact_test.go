package notify

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactRemovesEverySecretSubstring(t *testing.T) {
	got := Redact(errors.New("request to https://example.com/bot-token?chat=chat-id failed with secret"), "bot-token", "chat-id", "https://example.com", "secret")
	for _, secret := range []string{"bot-token", "chat-id", "https://example.com", "secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("Redact retained %q in %q", secret, got)
		}
	}
}

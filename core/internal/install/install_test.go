package install

import "testing"

func TestCommand(t *testing.T) {
	got := Command("https://192.0.2.10:8443", "tl_enr_abc", "ff00")
	want := "curl -fsSk https://192.0.2.10:8443/install.sh | sudo bash -s -- --url https://192.0.2.10:8443 --token tl_enr_abc --ca-fingerprint ff00"
	if got != want {
		t.Errorf("Command() = %q, want %q", got, want)
	}

	if got := Command("", "tl_enr_abc", "ff00"); got != "" {
		t.Errorf("Command() without an address = %q, want empty", got)
	}
}

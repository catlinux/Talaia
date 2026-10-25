package notify

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/catlinux/Talaia/core/internal/store"
)

func TestBuildMessageLocalizesEveryRuleAndLanguage(t *testing.T) {
	rules := []string{"server_offline", "cpu_high", "mem_high", "disk_high", "container_down", "container_unhealthy", "check_failed", "cert_expiry"}
	for _, lang := range []string{"ca", "es", "en"} {
		for _, rule := range rules {
			for _, event := range []string{"firing", "resolved"} {
				t.Run(lang+"/"+rule+"/"+event, func(t *testing.T) {
					a := store.Alert{ID: "alert", RuleType: rule, Severity: "critical", SubjectName: "sample", Stack: "stack", Summary: json.RawMessage(`{"key":"alerts.` + rule + `","params":{"value":82,"days":9}}`)}
					m := BuildMessage(lang, event, a, "example-server")
					// Checks are not tied to a server: they name the check instead.
					want := "example-server"
					if rule == "check_failed" || rule == "cert_expiry" {
						want = "sample"
					}
					for name, text := range map[string]string{"title": m.Title, "body": m.Body} {
						if strings.Contains(text, "<no value>") || strings.HasPrefix(text, "notify.") {
							t.Fatalf("%s untranslated: %q", name, text)
						}
						if !strings.Contains(text, want) {
							t.Errorf("%s does not contain %q: %q", name, want, text)
						}
					}
				})
			}
		}
	}
}

func TestBuildMessageReadsNaturally(t *testing.T) {
	down := store.Alert{RuleType: "container_down", Severity: "critical", SubjectName: "shop-web-1", Stack: "shop", Summary: json.RawMessage(`{"key":"alerts.container_down","params":{}}`)}
	if got := BuildMessage("en", "firing", down, "vps-web-01"); got.Title != "[CRITICAL] Container shop-web-1 is down on vps-web-01" || got.Body != "The container shop-web-1 (stack shop) on vps-web-01 is not running." {
		t.Errorf("container down = %q / %q", got.Title, got.Body)
	}
	disk := store.Alert{RuleType: "disk_high", Severity: "critical", Summary: json.RawMessage(`{"key":"alerts.disk_high","params":{"value":93}}`)}
	if got := BuildMessage("es", "firing", disk, "vps-web-01"); got.Body != "El disco raíz de vps-web-01 está al 93 %." {
		t.Errorf("disk = %q", got.Body)
	}
	cert := store.Alert{RuleType: "cert_expiry", Severity: "warning", SubjectName: "api.example.com", Summary: json.RawMessage(`{"key":"alerts.cert_expiry","params":{"days":-2}}`)}
	if got := BuildMessage("en", "firing", cert, ""); got.Body != "The certificate of api.example.com has expired." {
		t.Errorf("expired certificate = %q", got.Body)
	}
}

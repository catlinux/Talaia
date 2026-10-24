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
					a := store.Alert{ID: "alert", RuleType: rule, Severity: "critical", SubjectName: "sample", Stack: "stack", Summary: json.RawMessage(`{"key":"alerts.cpu_high","params":{"value":82}}`)}
					m := BuildMessage(lang, event, a, "example-server")
					for name, text := range map[string]string{"title": m.Title, "body": m.Body} {
						if strings.Contains(text, "<no value>") || strings.HasPrefix(text, "notify.") {
							t.Fatalf("%s untranslated: %q", name, text)
						}
						if !strings.Contains(text, "example-server") {
							t.Errorf("%s does not contain server name: %q", name, text)
						}
					}
				})
			}
		}
	}
}

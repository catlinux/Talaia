package notify

import (
	"encoding/json"
	"fmt"
	"github.com/catlinux/Talaia/core/i18n"
	"github.com/catlinux/Talaia/core/internal/store"
	"strings"
	"time"
)

func BuildMessage(lang, event string, a store.Alert, server string) Message {
	l, _ := i18n.New(lang)
	if l == nil {
		return Message{Event: event}
	}
	var summary map[string]any
	_ = json.Unmarshal(a.Summary, &summary)
	params, _ := summary["params"].(map[string]any)
	data := map[string]any{"Server": server, "Subject": a.SubjectName, "Severity": strings.ToUpper(a.Severity), "Stack": a.Stack}
	for k, v := range params {
		data[k] = v
	}
	if data["Server"] == "" {
		data["Server"] = a.SubjectName
	}
	key := "notify." + a.RuleType + "." + event
	title := l.T(key+"_title", data)
	body := l.T(key+"_body", data)
	if title == key+"_title" {
		title = fmt.Sprintf("[%s] %s on %s", strings.ToUpper(a.Severity), a.RuleType, server)
	}
	return Message{Event: event, Severity: a.Severity, Title: title, Body: body, ID: a.ID, RuleType: a.RuleType, ServerID: value(a.ServerID), ServerName: server, SubjectKind: a.SubjectKind, SubjectName: a.SubjectName, Stack: a.Stack, Summary: string(a.Summary), StartedAt: a.StartedAt, ResolvedAt: timeValue(a.ResolvedAt)}
}
func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func timeValue(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

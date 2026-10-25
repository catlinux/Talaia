package notify

import (
	"encoding/json"
	"fmt"
	"github.com/catlinux/Talaia/core/i18n"
	"github.com/catlinux/Talaia/core/internal/store"
	"math"
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
	data := map[string]any{"Server": server, "Subject": a.SubjectName, "Severity": strings.ToUpper(a.Severity), "Stack": a.Stack, "value": 0, "days": 0}
	for k, v := range params {
		// JSON numbers arrive as float64: whole ones print without a decimal point.
		if f, ok := v.(float64); ok && f == math.Trunc(f) {
			v = int64(f)
		}
		data[k] = v
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

package notify

import (
	"context"
	"net/http"
	"time"
)

type Message struct {
	Event, Severity, Title, Body             string
	ID, RuleType, ServerID, ServerName       string
	SubjectKind, SubjectName, Stack, Summary string
	StartedAt, ResolvedAt                    time.Time
}

type Sender interface {
	Send(context.Context, Message) error
}
type HTTPClientFactory func(func() bool) *http.Client

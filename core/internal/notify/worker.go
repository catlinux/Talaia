package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/catlinux/Talaia/core/i18n"
	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
	"net/http"
	"sync"
	"time"
)

type Store interface {
	ClaimDue(context.Context, time.Time, int) ([]store.NotificationDelivery, error)
	GetNotificationChannel(context.Context, string) (store.NotificationChannel, error)
	GetChannelSecret(context.Context, string) ([]byte, error)
	GetAlert(context.Context, string) (store.Alert, error)
	GetServer(context.Context, string) (store.Server, error)
	SetChannelStatus(context.Context, string, bool, string, time.Time) error
	MarkDeliverySent(context.Context, string, time.Time) error
	MarkDeliveryRetry(context.Context, string, time.Time, string) error
	MarkDeliveryFailed(context.Context, string, string) error
	GetAllowPrivateTargets(context.Context) (bool, error)
}
type Options struct {
	Store             Store
	Box               *auth.SecretBox
	Clock             func() time.Time
	AllowPrivate      func() bool
	HTTPClientFactory HTTPClientFactory
	I18nFactory       func(string) (any, error)
}
type Worker struct {
	opts          Options
	wake          chan struct{}
	mu            sync.Mutex
	allowed       bool
	allowedAt     time.Time
	senderFactory func(store.NotificationChannel, map[string]string, func() bool, HTTPClientFactory) Sender
}

func New(o Options) *Worker {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.HTTPClientFactory == nil {
		o.HTTPClientFactory = client
	}
	return &Worker{opts: o, wake: make(chan struct{}, 1), senderFactory: senderFor}
}
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		w.process(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}
func (w *Worker) private(ctx context.Context) bool {
	if w.opts.AllowPrivate != nil {
		return w.opts.AllowPrivate()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opts.Clock().Sub(w.allowedAt) > time.Minute {
		v, e := w.opts.Store.GetAllowPrivateTargets(ctx)
		if e == nil {
			w.allowed = v
		}
		w.allowedAt = w.opts.Clock()
	}
	return w.allowed
}
func (w *Worker) process(ctx context.Context) {
	ds, e := w.opts.Store.ClaimDue(ctx, w.opts.Clock(), 10)
	if e != nil {
		return
	}
	for _, d := range ds {
		w.deliver(ctx, d)
	}
}
func (w *Worker) deliver(ctx context.Context, d store.NotificationDelivery) {
	if d.ChannelID == nil {
		_ = w.opts.Store.MarkDeliveryFailed(ctx, d.ID, "channel removed")
		return
	}
	ch, e := w.opts.Store.GetNotificationChannel(ctx, *d.ChannelID)
	if e != nil {
		_ = w.opts.Store.MarkDeliveryFailed(ctx, d.ID, "channel removed")
		return
	}
	if !ch.Enabled {
		_ = w.opts.Store.MarkDeliveryFailed(ctx, d.ID, "channel disabled")
		return
	}
	sealed, e := w.opts.Store.GetChannelSecret(ctx, ch.ID)
	if e != nil {
		w.fail(ctx, d, ch, e)
		return
	}
	plain, e := w.opts.Box.Open(sealed)
	if e != nil {
		w.fail(ctx, d, ch, e)
		return
	}
	var sec map[string]string
	_ = json.Unmarshal(plain, &sec)
	var a store.Alert
	server := ""
	if d.AlertID != nil {
		a, e = w.opts.Store.GetAlert(ctx, *d.AlertID)
		if e != nil {
			w.fail(ctx, d, ch, e)
			return
		}
		if a.ServerID != nil {
			s, err := w.opts.Store.GetServer(ctx, *a.ServerID)
			if err == nil {
				server = s.Name
			}
		}
	}
	var m Message
	if d.Event == "test" {
		m = testMessage(ch.Language)
	} else {
		m = BuildMessage(ch.Language, d.Event, a, server)
	}
	allow := func() bool { return w.private(ctx) }
	sender := w.senderFactory(ch, sec, allow, w.opts.HTTPClientFactory)
	err := invoke(sender, ctx, m)
	if err != nil {
		w.fail(ctx, d, ch, err, secretValues(sec)...)
		return
	}
	_ = w.opts.Store.MarkDeliverySent(ctx, d.ID, w.opts.Clock())
	_ = w.opts.Store.SetChannelStatus(ctx, ch.ID, true, "", w.opts.Clock())
}
func (w *Worker) fail(ctx context.Context, d store.NotificationDelivery, ch store.NotificationChannel, e error, secrets ...string) {
	msg := Redact(e, secrets...)
	_ = w.opts.Store.SetChannelStatus(ctx, ch.ID, false, msg, w.opts.Clock())
	if d.Attempts >= 5 {
		_ = w.opts.Store.MarkDeliveryFailed(ctx, d.ID, msg)
		return
	}
	back := []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute}
	idx := d.Attempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(back) {
		idx = len(back) - 1
	}
	_ = w.opts.Store.MarkDeliveryRetry(ctx, d.ID, w.opts.Clock().Add(back[idx]), msg)
}
func invoke(s Sender, ctx context.Context, m Message) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("sender failed")
		}
	}()
	return s.Send(ctx, m)
}
func (w *Worker) SendTest(ctx context.Context, id string) error {
	ch, e := w.opts.Store.GetNotificationChannel(ctx, id)
	if e != nil {
		return errors.New("channel unavailable")
	}
	if !ch.Enabled {
		return errors.New("channel disabled")
	}
	sealed, e := w.opts.Store.GetChannelSecret(ctx, id)
	if e != nil {
		return errors.New(Redact(e))
	}
	plain, e := w.opts.Box.Open(sealed)
	if e != nil {
		return errors.New(Redact(e))
	}
	var sec map[string]string
	_ = json.Unmarshal(plain, &sec)
	m := testMessage(ch.Language)
	allow := func() bool { return w.private(ctx) }
	e = invoke(w.senderFactory(ch, sec, allow, w.opts.HTTPClientFactory), ctx, m)
	if e != nil {
		msg := Redact(e, secretValues(sec)...)
		_ = w.opts.Store.SetChannelStatus(ctx, id, false, msg, w.opts.Clock())
		return errors.New(msg)
	}
	_ = w.opts.Store.SetChannelStatus(ctx, id, true, "", w.opts.Clock())
	return nil
}

func testMessage(lang string) Message {
	body := "Talaia test notification"
	if l, err := i18n.New(lang); err == nil && l != nil {
		body = l.T("notify.test", nil)
	}
	return Message{Event: "test", Title: "Talaia test notification", Body: body, Severity: "warning"}
}

func secretValues(secrets map[string]string) []string {
	values := make([]string, 0, len(secrets))
	for _, value := range secrets {
		values = append(values, value)
	}
	return values
}
func senderFor(ch store.NotificationChannel, s map[string]string, allow func() bool, f HTTPClientFactory) Sender {
	conf := map[string]any{}
	_ = json.Unmarshal(ch.Config, &conf)
	str := func(k string) string { v, _ := conf[k].(string); return v }
	var c *http.Client
	if f != nil {
		c = f(allow)
	}
	switch ch.Type {
	case "telegram":
		return Telegram{Token: s["token"], ChatID: str("chat_id"), Client: c}
	case "ntfy":
		n := Ntfy{Server: str("server"), Topic: s["topic"], Token: s["token"], Client: c}
		if v, ok := conf["priority"].(float64); ok {
			n.Priority = int(v)
		}
		return n
	case "discord":
		return Discord{URL: s["url"], Client: c}
	case "webhook":
		return Webhook{URL: s["url"], HMACSecret: s["hmac_secret"], Client: c}
	case "email":
		tos := []string{}
		if v, ok := conf["to"].([]any); ok {
			for _, x := range v {
				tos = append(tos, fmt.Sprint(x))
			}
		}
		return Email{Host: str("host"), From: str("from"), To: tos, Username: s["username"], Password: s["password"], TLSMode: str("tls_mode"), AllowPrivate: allow}
	default:
		return invalidSender{}
	}
}

type invalidSender struct{}

func (invalidSender) Send(context.Context, Message) error {
	return fmt.Errorf("unsupported channel type")
}

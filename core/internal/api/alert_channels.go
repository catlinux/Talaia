package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/catlinux/Talaia/core/internal/notify"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

type alertChannelJSON struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Enabled   bool            `json:"enabled"`
	Language  string          `json:"language"`
	Config    json.RawMessage `json:"config"`
	HasSecret bool            `json:"has_secret"`
	LastError string          `json:"last_error"`
	LastOKAt  *time.Time      `json:"last_ok_at"`
}

func alertChannelEntry(v store.NotificationChannel) alertChannelJSON {
	config := v.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	return alertChannelJSON{v.ID, v.Name, v.Type, v.Enabled, v.Language, config, v.HasSecret, v.LastError, v.LastOKAt}
}

func alertChannelResponse(r *http.Request, d AlertsDeps, v store.NotificationChannel) alertChannelJSON {
	entry := alertChannelEntry(v)
	if d.Box == nil {
		entry.LastError = ""
		return entry
	}
	if !v.HasSecret {
		return entry
	}
	sealed, err := d.Store.GetChannelSecret(r.Context(), v.ID)
	if err != nil {
		entry.LastError = ""
		return entry
	}
	plain, err := d.Box.Open(sealed)
	if err != nil {
		entry.LastError = ""
		return entry
	}
	parsed, err := notify.ParseSecret(v.Type, plain)
	if err != nil {
		entry.LastError = ""
		return entry
	}
	if values, ok := parsed.(map[string]string); ok {
		secrets := make([]string, 0, len(values))
		for _, value := range values {
			secrets = append(secrets, value)
		}
		entry.LastError = notify.Redact(errors.New(entry.LastError), secrets...)
	}
	return entry
}

type alertChannelRequest struct {
	Name     *string         `json:"name"`
	Type     *string         `json:"type"`
	Enabled  *bool           `json:"enabled"`
	Language *string         `json:"language"`
	Config   json.RawMessage `json:"config"`
	Secret   json.RawMessage `json:"secret"`
}

func NewAlertChannelsHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.Store.ListNotificationChannels(r.Context())
		if err != nil {
			internal(w, "list notification channels", err)
			return
		}
		out := make([]alertChannelJSON, 0, len(rows))
		for _, row := range rows {
			out = append(out, alertChannelResponse(r, d, row))
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func NewAlertChannelCreateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in alertChannelRequest
		if decodeJSON(w, r, &in) != nil {
			writeInvalid(w, "", "body")
			return
		}
		v := store.NotificationChannel{Enabled: true, Language: "en", Config: json.RawMessage(`{}`)}
		applyAlertChannelRequest(&v, in)
		secret, ok := alertChannelSecret(w, v.Type, in.Secret)
		if !ok {
			return
		}
		if !validAlertChannel(w, v, secret) {
			return
		}
		rows, err := d.Store.ListNotificationChannels(r.Context())
		if err != nil {
			internal(w, "list notification channels", err)
			return
		}
		for _, row := range rows {
			if row.Name == v.Name {
				writeInvalid(w, "name", "already_exists")
				return
			}
		}
		if d.Box == nil {
			internal(w, "seal notification channel secret", errors.New("nil secret box"))
			return
		}
		session, _ := sessionFromContext(r.Context())
		v.UpdatedBy = session.User.Username
		created, err := d.Store.CreateNotificationChannel(r.Context(), v, d.Box.Seal(secret))
		if err != nil {
			internal(w, "create notification channel", err)
			return
		}
		auditAlert(d, r, "channel_create", rbac.Target{}, map[string]any{"name": created.Name, "type": created.Type})
		writeJSON(w, http.StatusCreated, alertChannelResponse(r, d, created))
	})
}

func NewAlertChannelUpdateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "channelID")
		v, err := d.Store.GetNotificationChannel(r.Context(), id)
		if err != nil {
			alertChannelStoreError(w, "get notification channel", err)
			return
		}
		var in alertChannelRequest
		if decodeJSON(w, r, &in) != nil {
			writeInvalid(w, "", "body")
			return
		}
		applyAlertChannelRequest(&v, in)
		var secret []byte
		if len(in.Secret) != 0 {
			var ok bool
			secret, ok = alertChannelSecret(w, v.Type, in.Secret)
			if !ok {
				return
			}
		} else {
			sealed, e := d.Store.GetChannelSecret(r.Context(), id)
			if e != nil {
				alertChannelStoreError(w, "get notification channel secret", e)
				return
			}
			if d.Box == nil {
				internal(w, "open notification channel secret", errors.New("nil secret box"))
				return
			}
			secret, err = d.Box.Open(sealed)
			if err != nil {
				internal(w, "open notification channel secret", err)
				return
			}
		}
		if !validAlertChannel(w, v, secret) {
			return
		}
		rows, err := d.Store.ListNotificationChannels(r.Context())
		if err != nil {
			internal(w, "list notification channels", err)
			return
		}
		for _, row := range rows {
			if row.ID != id && row.Name == v.Name {
				writeInvalid(w, "name", "already_exists")
				return
			}
		}
		session, _ := sessionFromContext(r.Context())
		v.UpdatedBy = session.User.Username
		var sealed []byte
		if len(in.Secret) != 0 {
			sealed = d.Box.Seal(secret)
		}
		if err = d.Store.UpdateNotificationChannel(r.Context(), v, sealed); err != nil {
			alertChannelStoreError(w, "update notification channel", err)
			return
		}
		auditAlert(d, r, "channel_update", rbac.Target{}, map[string]any{"name": v.Name, "type": v.Type})
		v.HasSecret = true
		writeJSON(w, http.StatusOK, alertChannelResponse(r, d, v))
	})
}

func NewAlertChannelDeleteHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "channelID")
		v, err := d.Store.GetNotificationChannel(r.Context(), id)
		if err != nil {
			alertChannelStoreError(w, "get notification channel", err)
			return
		}
		if err = d.Store.DeleteNotificationChannel(r.Context(), id); err != nil {
			alertChannelStoreError(w, "delete notification channel", err)
			return
		}
		auditAlert(d, r, "channel_delete", rbac.Target{}, map[string]any{"name": v.Name, "type": v.Type})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func NewAlertChannelTestHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "channelID")
		v, err := d.Store.GetNotificationChannel(r.Context(), id)
		if err != nil {
			alertChannelStoreError(w, "get notification channel", err)
			return
		}
		if d.Notifier == nil {
			internal(w, "test notification channel", errors.New("nil notifier"))
			return
		}
		err = d.Notifier.SendTest(r.Context(), id)
		auditAlert(d, r, "channel_test", rbac.Target{}, map[string]any{"name": v.Name, "type": v.Type})
		if err != nil {
			sealed, e := d.Store.GetChannelSecret(r.Context(), id)
			if e == nil && d.Box != nil {
				if plain, openErr := d.Box.Open(sealed); openErr == nil {
					if parsed, parseErr := notify.ParseSecret(v.Type, plain); parseErr == nil {
						if values, ok := parsed.(map[string]string); ok {
							secrets := make([]string, 0, len(values))
							for _, value := range values {
								secrets = append(secrets, value)
							}
							err = errors.New(notify.Redact(err, secrets...))
						}
					}
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": notify.Redact(err)})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

func applyAlertChannelRequest(v *store.NotificationChannel, in alertChannelRequest) {
	if in.Name != nil {
		v.Name = *in.Name
	}
	if in.Type != nil {
		v.Type = *in.Type
	}
	if in.Enabled != nil {
		v.Enabled = *in.Enabled
	}
	if in.Language != nil {
		v.Language = *in.Language
	}
	if in.Config != nil {
		v.Config = in.Config
	}
}

func alertChannelSecret(w http.ResponseWriter, typ string, raw json.RawMessage) ([]byte, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		writeInvalid(w, "secret", "required")
		return nil, false
	}
	if _, err := notify.ParseSecret(typ, raw); err != nil {
		writeInvalid(w, "secret", "invalid_value")
		return nil, false
	}
	return raw, true
}

func validAlertChannel(w http.ResponseWriter, v store.NotificationChannel, secret []byte) bool {
	if n := utf8.RuneCountInString(v.Name); n < 1 || n > 80 {
		writeInvalid(w, "name", "out_of_range")
		return false
	}
	if v.Language != "ca" && v.Language != "es" && v.Language != "en" {
		writeInvalid(w, "language", "invalid_value")
		return false
	}
	if err := notify.ValidateChannel(v.Type, v.Config, secret); err != nil {
		writeInvalid(w, "config", "invalid_value")
		return false
	}
	return true
}

func alertChannelStoreError(w http.ResponseWriter, action string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	internal(w, action, fmt.Errorf("%s: %w", action, err))
}

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/catlinux/Talaia/core/internal/store"
)

// testVersion is the running version the test panel reports.
const testVersion = "1.2.3"

type fakePinger struct {
	err error
}

func (f fakePinger) Ping(context.Context) error {
	return f.err
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "healthy",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "pinger fails",
			pingErr:    errors.New("database unreachable"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"unavailable"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(fakePinger{err: tt.pingErr})
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", contentType)
			}
			if body := strings.TrimSpace(rec.Body.String()); body != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

type deadlinePinger struct {
	t *testing.T
}

func (p deadlinePinger) Ping(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		p.t.Error("Ping context has no deadline")
	}
	return nil
}

func TestHealthzSetsPingDeadline(t *testing.T) {
	handler := NewHandler(deadlinePinger{t: t})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSessionVersionHeaderOnlyForFullSessions(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := sessionVersion(testVersion)(next)
	tests := []struct {
		name    string
		session *Session
		want    bool
	}{
		{name: "anonymous"},
		{name: "pending second factor", session: &Session{Stage: store.StageMFAPending}},
		{name: "full session", session: &Session{Stage: store.StageFull}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
			if test.session != nil {
				request = request.WithContext(withSession(request.Context(), *test.session))
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			values := recorder.Header().Values(versionHeaderName)
			if test.want {
				if len(values) != 1 || values[0] != testVersion {
					t.Fatalf("%s = %v, want [%s]", versionHeaderName, values, testVersion)
				}
				return
			}
			if len(values) != 0 {
				t.Fatalf("%s = %v, want no header", versionHeaderName, values)
			}
		})
	}

	empty := sessionVersion("")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	request = request.WithContext(withSession(request.Context(), Session{Stage: store.StageFull}))
	recorder := httptest.NewRecorder()
	empty(next).ServeHTTP(recorder, request)
	values := recorder.Header().Values(versionHeaderName)
	if len(values) != 1 || values[0] != "" {
		t.Fatalf("%s = %v, want an empty value", versionHeaderName, values)
	}
}

func TestVersionHeaderAbsentOnPublicRoutes(t *testing.T) {
	router := NewRouter(RouterDeps{
		DB: fakePinger{}, WebDir: t.TempDir(), Version: testVersion,
		Authz: NewAuthorizer(AuthorizerDeps{}),
	})
	for _, path := range []string{"/healthz", "/index.html", "/api/v1/auth/session"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if values := recorder.Header().Values(versionHeaderName); len(values) != 0 {
				t.Fatalf("%s = %v, want no header", versionHeaderName, values)
			}
		})
	}
}

func TestVersionHeaderOnAnAuthenticatedAPI(t *testing.T) {
	router := NewRouter(RouterDeps{
		DB: fakePinger{}, WebDir: t.TempDir(), Version: testVersion,
		Authz: NewAuthorizer(AuthorizerDeps{}),
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	request = request.WithContext(withSession(request.Context(), Session{
		Stage: store.StageFull, User: store.User{Username: "admin"},
	}))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("session status = %d", recorder.Code)
	}
	if got := recorder.Header().Get(versionHeaderName); got != testVersion {
		t.Fatalf("%s = %q, want %q", versionHeaderName, got, testVersion)
	}
}

func TestVersionHeaderOnAnAuthenticatedResponse(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	response := panel.do(t, panel.client, http.MethodGet, "/api/v1/auth/session", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d", response.StatusCode)
	}
	if got := response.Header.Get(versionHeaderName); got != testVersion {
		t.Fatalf("%s = %q, want %q", versionHeaderName, got, testVersion)
	}
}

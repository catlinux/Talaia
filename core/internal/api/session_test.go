package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

// testPasswordHash is a syntactically valid PHC string: the session code never
// looks inside it.
const testPasswordHash = "$argon2id$v=19$m=65536,t=3,p=1$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"

// newSessionFixture creates the user and one session of the given stage.
func newSessionFixture(t *testing.T, stage string) (*store.Store, *testClock, store.User, string) {
	t.Helper()

	st := testStore(t)
	clock := newTestClock()
	ctx := context.Background()

	id, err := st.CreateUser(ctx, "admin", testPasswordHash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, err := st.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	token := auth.NewSessionToken()
	expires := clock.Now().Add(fullSessionTTL)
	if stage == store.StageMFAPending {
		expires = clock.Now().Add(pendingSessionTTL)
	}
	if _, err := st.CreateSession(ctx, id, auth.HashToken(token), stage, expires, "192.0.2.10", "test"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	return st, clock, user, token
}

// requestWithToken returns a request carrying the session cookie.
func requestWithToken(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	if token != "" {
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	}
	return request
}

func TestSessionsMiddlewareAttachesTheSession(t *testing.T) {
	st, clock, user, token := newSessionFixture(t, store.StageFull)

	var got Session
	handler := Sessions(SessionDeps{Store: st, Now: clock.Now, IdleTimeout: 12 * time.Hour})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, _ = sessionFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithToken(token))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got.User.ID != user.ID || got.User.Username != "admin" {
		t.Errorf("session user = %+v, want the created user", got.User)
	}
	if got.Stage != store.StageFull {
		t.Errorf("stage = %q, want %q", got.Stage, store.StageFull)
	}

	// The cookie is refreshed while the session is in use.
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != SessionCookieName {
		t.Fatalf("the response did not refresh the session cookie")
	}
	if want := int(fullSessionTTL / time.Second); cookies[0].MaxAge != want {
		t.Errorf("Max-Age = %d, want %d", cookies[0].MaxAge, want)
	}

	// After a couple of minutes the activity is recorded.
	clock.Advance(2 * time.Minute)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithToken(token))
	stored, err := st.GetSessionByTokenHash(context.Background(), auth.HashToken(token))
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !stored.LastSeenAt.After(user.CreatedAt) {
		t.Errorf("last_seen_at = %s, want it recorded after the creation", stored.LastSeenAt)
	}
}

func TestSessionsMiddlewareLeavesBadSessionsAnonymous(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, st *store.Store, clock *testClock, token string) string
	}{
		{
			name: "unknown token",
			setup: func(*testing.T, *store.Store, *testClock, string) string {
				return "a token that was never issued"
			},
		},
		{
			name: "revoked session",
			setup: func(t *testing.T, st *store.Store, _ *testClock, token string) string {
				ctx := context.Background()
				session, err := st.GetSessionByTokenHash(ctx, auth.HashToken(token))
				if err != nil {
					t.Fatalf("get session: %v", err)
				}
				if err := st.RevokeSession(ctx, session.ID); err != nil {
					t.Fatalf("revoke session: %v", err)
				}
				return token
			},
		},
		{
			name: "expired session",
			setup: func(t *testing.T, st *store.Store, clock *testClock, _ string) string {
				ctx := context.Background()
				user, err := st.GetUserByUsername(ctx, "admin")
				if err != nil {
					t.Fatalf("get user: %v", err)
				}
				token := auth.NewSessionToken()
				if _, err := st.CreateSession(ctx, user.ID, auth.HashToken(token), store.StageFull,
					clock.Now().Add(-time.Minute), "", ""); err != nil {
					t.Fatalf("create the expired session: %v", err)
				}
				return token
			},
		},
		{
			name: "idle session",
			setup: func(_ *testing.T, _ *store.Store, clock *testClock, token string) string {
				clock.Advance(13 * time.Hour)
				return token
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, clock, _, token := newSessionFixture(t, store.StageFull)
			token = tt.setup(t, st, clock, token)

			attached := false
			handler := Sessions(SessionDeps{Store: st, Now: clock.Now, IdleTimeout: 12 * time.Hour})(
				http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					_, attached = sessionFromContext(r.Context())
				}))

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, requestWithToken(token))

			if attached {
				t.Error("a session was attached to a request that must be anonymous")
			}
			// The stale cookie is dropped.
			cookies := recorder.Result().Cookies()
			if len(cookies) == 0 || cookies[0].Name != SessionCookieName || cookies[0].MaxAge >= 0 {
				t.Errorf("the stale cookie was not cleared: %v", cookies)
			}
		})
	}
}

func TestRequireFull(t *testing.T) {
	tests := []struct {
		name    string
		session *Session
		want    int
		code    string
	}{
		{name: "no session", want: http.StatusUnauthorized, code: "session_required"},
		{name: "pending session", session: &Session{Stage: store.StageMFAPending}, want: http.StatusUnauthorized, code: "mfa_required"},
		{name: "full session", session: &Session{Stage: store.StageFull}, want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := RequireFull(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			ctx := context.Background()
			if tt.session != nil {
				ctx = withSession(ctx, *tt.session)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil).WithContext(ctx))

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
			if tt.code != "" {
				var body errorBody
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode the body: %v", err)
				}
				if body.Error.Code != tt.code {
					t.Errorf("code = %q, want %q", body.Error.Code, tt.code)
				}
			}
		})
	}
}

func TestPendingSessionCannotChangeThePassword(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.login(t, "admin", testPassword)

	response := panel.post(t, "/api/v1/auth/password", passwordRequest{
		CurrentPassword: testPassword,
		NewPassword:     "another good password",
		Code:            "000000",
	})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.StatusCode)
	}
	var body errorBody
	decodeBody(t, response, &body)
	if body.Error.Code != "mfa_required" {
		t.Errorf("code = %q, want mfa_required", body.Error.Code)
	}
}

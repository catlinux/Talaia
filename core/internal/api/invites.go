package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

type inviteCreateRequest struct {
	Username string     `json:"username"`
	Email    string     `json:"email"`
	Role     string     `json:"role"`
	Scope    adminScope `json:"scope"`
}

type inviteCreatedResponse struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	LinkPath  string    `json:"link_path"`
	ExpiresAt time.Time `json:"expires_at"`
}

type inviteEntry struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	Scope     adminScope `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
}

type invitesResponse struct {
	Invites []inviteEntry `json:"invites"`
}

func inviteScopeOf(invite store.Invite) adminScope {
	return adminScope{
		Type:     invite.ScopeType,
		ServerID: invite.ScopeServerID,
		GroupID:  invite.ScopeGroupID,
		Name:     invite.ScopeName,
	}
}

func inviteEntryOf(invite store.Invite) inviteEntry {
	return inviteEntry{
		ID: invite.ID, Username: invite.Username, Email: invite.Email, Role: invite.Role,
		Scope: inviteScopeOf(invite), CreatedAt: invite.CreatedAt, ExpiresAt: invite.ExpiresAt,
	}
}

func (h *userAdminHandler) createInvite(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request inviteCreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	request.Email = strings.TrimSpace(request.Email)
	request.Role = strings.TrimSpace(request.Role)
	if request.Username == "" || !validAdminRole(request.Role) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	scope, err := h.validScope(r, request.Scope)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		internal(w, "generate invite token", err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	invite, err := st.CreateInvite(r.Context(), store.Invite{
		TokenHash:     auth.HashToken(token),
		Username:      request.Username,
		Email:         request.Email,
		Role:          request.Role,
		ScopeType:     scope.Type,
		ScopeServerID: scope.ServerID,
		ScopeGroupID:  scope.GroupID,
		ScopeName:     scope.Name,
		CreatedBy:     sessionUsername(r),
	})
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	h.audit(r, "invite_create", map[string]any{"type": "organization"}, map[string]any{
		"invite_id": invite.ID, "username": invite.Username, "role": invite.Role, "scope": scope,
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, inviteCreatedResponse{
		ID: invite.ID, Token: token, LinkPath: "/invite#" + token, ExpiresAt: invite.ExpiresAt,
	})
}

func (h *userAdminHandler) listInvites(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	invites, err := st.ListPendingInvites(r.Context())
	if err != nil {
		internal(w, "list pending invites", err)
		return
	}
	response := invitesResponse{Invites: make([]inviteEntry, 0, len(invites))}
	for _, invite := range invites {
		response.Invites = append(response.Invites, inviteEntryOf(invite))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) revokeInvite(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	inviteID := chi.URLParam(r, "inviteID")
	if err := st.RevokeInvite(r.Context(), inviteID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "invite_revoke", map[string]any{"type": "invite", "invite_id": inviteID},
		map[string]any{"invite_id": inviteID})
	w.WriteHeader(http.StatusNoContent)
}

type inviteInfoResponse struct {
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
}

type inviteAcceptRequest struct {
	Password string `json:"password"`
}

func (h *authHandler) inviteInfo(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	invite, ok, err := h.pendingInvite(r, chi.URLParam(r, "token"))
	if err != nil {
		internal(w, "get invite", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "invalid_invite")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, inviteInfoResponse{Username: invite.Username, ExpiresAt: invite.ExpiresAt})
}

func (h *authHandler) inviteAccept(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	var request inviteAcceptRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	invite, ok, err := h.pendingInvite(r, chi.URLParam(r, "token"))
	if err != nil {
		internal(w, "get invite", err)
		return
	}
	if !ok {
		// Missing, used and expired tokens look exactly the same.
		writeError(w, http.StatusNotFound, "invalid_invite")
		return
	}
	if err := auth.ValidatePasswordPolicy(request.Password); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password")
		return
	}

	user, err := h.deps.Store.ConsumeInvite(r.Context(), invite.TokenHash, auth.HashPassword(request.Password))
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInviteInvalid), errors.Is(err, store.ErrInviteExpired):
			writeError(w, http.StatusNotFound, "invalid_invite")
		case errors.Is(err, store.ErrUserExists):
			writeError(w, http.StatusConflict, "username_taken")
		case errors.Is(err, store.ErrEmailExists):
			writeError(w, http.StatusConflict, "email_taken")
		default:
			internal(w, "consume invite", err)
		}
		return
	}

	token := auth.NewSessionToken()
	sessionID, err := h.deps.Store.CreateSession(r.Context(), user.ID, auth.HashToken(token),
		store.StageMFAPending, h.now().Add(setupSessionTTL), clientIP(r), r.UserAgent())
	if err != nil {
		internal(w, "create invite setup session", err)
		return
	}
	http.SetCookie(w, sessionCookie(token, store.StageMFAPending))
	h.auditActor(r, user.ID, user.Username, "invite_accept", userScope(user.ID),
		map[string]any{"username": user.Username, "invite_id": invite.ID})

	// Continue with the same first-access flow as login: the invitee must set
	// the second factor before receiving a full session.
	session := Session{ID: sessionID, User: user, Stage: store.StageMFAPending}
	h.totpSetup(w, r.WithContext(withSession(r.Context(), session)))
}

func (h *authHandler) pendingInvite(r *http.Request, token string) (store.Invite, bool, error) {
	invite, err := h.deps.Store.GetInviteByTokenHash(r.Context(), auth.HashToken(strings.TrimSpace(token)))
	if errors.Is(err, store.ErrNotFound) {
		return store.Invite{}, false, nil
	}
	if err != nil {
		return store.Invite{}, false, err
	}
	if !invite.UsedAt.IsZero() || !invite.ExpiresAt.After(h.now()) {
		return store.Invite{}, false, nil
	}
	return invite, true, nil
}

func (h *authHandler) auditActor(r *http.Request, actorID, actor, action string, scope, details map[string]any) {
	if err := h.deps.Store.AppendAudit(r.Context(), store.AuditEvent{
		Actor: actor, ActorID: actorID, Action: action, IP: clientIP(r), Result: "success",
		Details: details, Scope: scope,
	}); err != nil {
		// The account was created and the invite consumed; never fail the
		// response because the audit write failed.
	}
}

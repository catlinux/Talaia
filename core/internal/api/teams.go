package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/store"
)

type teamRequest struct {
	Name string `json:"name"`
}

type teamEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type teamsResponse struct {
	Teams []teamEntry `json:"teams"`
}

type teamDetail struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Members  []string       `json:"members"`
	Bindings []adminBinding `json:"bindings"`
}

func teamEntryOf(team store.Team) teamEntry {
	return teamEntry{ID: team.ID, Name: team.Name}
}

func (h *userAdminHandler) listTeams(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	teams, err := st.ListTeams(r.Context())
	if err != nil {
		internal(w, "list teams", err)
		return
	}
	response := teamsResponse{Teams: make([]teamEntry, 0, len(teams))}
	for _, team := range teams {
		response.Teams = append(response.Teams, teamEntryOf(team))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) createTeam(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request teamRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	if err := validateAdminName("name", request.Name); err != nil {
		writeValidationError(w, err)
		return
	}
	team, err := st.CreateTeam(r.Context(), request.Name)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "team_create", teamScope(team.ID), map[string]any{"team_id": team.ID, "name": team.Name})
	writeJSON(w, http.StatusCreated, teamEntryOf(team))
}

func (h *userAdminHandler) updateTeam(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request teamRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	if err := validateAdminName("name", request.Name); err != nil {
		writeValidationError(w, err)
		return
	}
	teamID := chi.URLParam(r, "teamID")
	if err := st.UpdateTeam(r.Context(), teamID, request.Name); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "team_update", teamScope(teamID), map[string]any{"team_id": teamID, "name": strings.TrimSpace(request.Name)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) deleteTeam(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	teamID := chi.URLParam(r, "teamID")
	if err := st.DeleteTeam(r.Context(), teamID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "team_delete", teamScope(teamID), map[string]any{"team_id": teamID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) getTeam(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	teamID := chi.URLParam(r, "teamID")
	team, err := st.GetTeam(r.Context(), teamID)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	members, err := st.ListTeamMembers(r.Context(), teamID)
	if err != nil {
		internal(w, "list team members", err)
		return
	}
	bindings, err := st.ListRoleBindings(r.Context(), store.SubjectTeam, teamID)
	if err != nil {
		internal(w, "list team bindings", err)
		return
	}
	response := teamDetail{
		ID: team.ID, Name: team.Name,
		Members: make([]string, 0, len(members)), Bindings: make([]adminBinding, 0, len(bindings)),
	}
	for _, member := range members {
		response.Members = append(response.Members, member.Username)
	}
	for _, binding := range bindings {
		response.Bindings = append(response.Bindings, adminBindingOf(binding))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) addTeamMember(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	teamID, userID := chi.URLParam(r, "teamID"), chi.URLParam(r, "userID")
	if err := st.AddTeamMember(r.Context(), teamID, userID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "team_member_add", teamScope(teamID), map[string]any{"team_id": teamID, "user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) removeTeamMember(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	teamID, userID := chi.URLParam(r, "teamID"), chi.URLParam(r, "userID")
	if err := st.RemoveTeamMember(r.Context(), teamID, userID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "team_member_remove", teamScope(teamID), map[string]any{"team_id": teamID, "user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) listTeamBindings(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	teamID := chi.URLParam(r, "teamID")
	if _, err := st.GetTeam(r.Context(), teamID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	bindings, err := st.ListRoleBindings(r.Context(), store.SubjectTeam, teamID)
	if err != nil {
		internal(w, "list team bindings", err)
		return
	}
	response := userBindingsResponse{Bindings: make([]adminBinding, 0, len(bindings))}
	for _, binding := range bindings {
		response.Bindings = append(response.Bindings, adminBindingOf(binding))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) createTeamBinding(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	teamID := chi.URLParam(r, "teamID")
	if _, err := st.GetTeam(r.Context(), teamID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	var request bindingRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	request.Role = strings.TrimSpace(request.Role)
	if !validAdminRole(request.Role) {
		writeInvalid(w, "role", "invalid_value")
		return
	}
	scope, err := h.validScope(r, request.Scope)
	if err != nil {
		writeValidationError(w, err)
		return
	}
	binding, err := st.CreateRoleBinding(r.Context(), store.RoleBinding{
		SubjectType:   store.SubjectTeam,
		SubjectID:     teamID,
		ScopeType:     scope.Type,
		ScopeServerID: scope.ServerID,
		ScopeGroupID:  scope.GroupID,
		ScopeName:     scope.Name,
		Role:          request.Role,
		CreatedBy:     sessionUsername(r),
	})
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "binding_create", teamScope(teamID), map[string]any{
		"team_id": teamID, "binding_id": binding.ID, "role": binding.Role, "scope": scope,
	})
	writeJSON(w, http.StatusCreated, adminBindingOf(binding))
}

func teamScope(teamID string) map[string]any {
	return map[string]any{"type": "team", "team_id": teamID}
}

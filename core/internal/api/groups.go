package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/store"
)

type groupRequest struct {
	Name string `json:"name"`
}

type groupEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type groupsResponse struct {
	Groups []groupEntry `json:"groups"`
}

type serverGroupRequest struct {
	GroupID *string `json:"group_id"`
}

func validAdminName(name string) bool {
	name = strings.TrimSpace(name)
	length := utf8.RuneCountInString(name)
	return length >= 1 && length <= 64
}

func validateAdminName(field, name string) error {
	name = strings.TrimSpace(name)
	length := utf8.RuneCountInString(name)
	if length == 0 {
		return fieldError{field, "required"}
	}
	if length > 64 {
		return fieldError{field, "too_long"}
	}
	return nil
}

func groupEntryOf(group store.ServerGroup) groupEntry {
	return groupEntry{ID: group.ID, Name: group.Name}
}

func (h *userAdminHandler) listGroups(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	groups, err := st.ListGroups(r.Context())
	if err != nil {
		internal(w, "list groups", err)
		return
	}
	response := groupsResponse{Groups: make([]groupEntry, 0, len(groups))}
	for _, group := range groups {
		response.Groups = append(response.Groups, groupEntryOf(group))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) createGroup(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request groupRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	if err := validateAdminName("name", request.Name); err != nil {
		writeValidationError(w, err)
		return
	}
	group, err := st.CreateGroup(r.Context(), request.Name)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "group_create", groupScope(group.ID), map[string]any{"group_id": group.ID, "name": group.Name})
	writeJSON(w, http.StatusCreated, groupEntryOf(group))
}

func (h *userAdminHandler) updateGroup(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request groupRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	if err := validateAdminName("name", request.Name); err != nil {
		writeValidationError(w, err)
		return
	}
	groupID := chi.URLParam(r, "groupID")
	if err := st.UpdateGroup(r.Context(), groupID, request.Name); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "group_update", groupScope(groupID), map[string]any{"group_id": groupID, "name": strings.TrimSpace(request.Name)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	groupID := chi.URLParam(r, "groupID")
	if err := st.DeleteGroup(r.Context(), groupID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "group_delete", groupScope(groupID), map[string]any{"group_id": groupID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) setServerGroup(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request serverGroupRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	serverID := chi.URLParam(r, "serverID")
	groupID := ""
	if request.GroupID != nil {
		groupID = strings.TrimSpace(*request.GroupID)
	}
	if err := st.SetServerGroup(r.Context(), serverID, groupID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "server_group_set", serverScope(serverID), map[string]any{
		"server_id": serverID, "group_id": groupID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func groupScope(groupID string) map[string]any {
	return map[string]any{"type": "group", "group_id": groupID}
}

func serverScope(serverID string) map[string]any {
	return map[string]any{"type": "server", "server_id": serverID}
}

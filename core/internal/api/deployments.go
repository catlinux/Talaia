package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/store"
)

var deploymentCommitRe = regexp.MustCompile(`^[a-f0-9]{40}$`)

type deploymentResponse struct {
	ID            string    `json:"id"`
	AppID         string    `json:"app_id"`
	Number        int       `json:"number"`
	Trigger       string    `json:"trigger"`
	CommitSHA     string    `json:"commit_sha"`
	CommitSubject string    `json:"commit_subject"`
	ImageRef      string    `json:"image_ref"`
	State         string    `json:"state"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	Error         string    `json:"error"`
}

type deploymentDetailResponse struct {
	deploymentResponse
	Log string `json:"log"`
}

type deploymentRequest struct {
	Commit string `json:"commit"`
}

func toDeploymentResponse(item store.Deployment) deploymentResponse {
	return deploymentResponse{
		ID: item.ID, AppID: item.AppID, Number: item.Number, Trigger: item.Trigger,
		CommitSHA: item.CommitSHA, CommitSubject: item.CommitSubject, ImageRef: item.ImageRef,
		State: item.State, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, Error: item.Error,
	}
}

func NewDeploymentListHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.listDeployments)
}

func NewDeploymentGetHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.getDeployment)
}

func NewDeploymentCreateHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.createDeployment)
}

func NewDeploymentRollbackHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.rollbackDeployment)
}

func (h appHandler) listDeployments(w http.ResponseWriter, r *http.Request) {
	app, err := h.app(r)
	if err != nil {
		appStoreError(w, err)
		return
	}
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	items, err := h.deps.Store.ListDeployments(r.Context(), app.ID, limit)
	if err != nil {
		internal(w, "list deployments", err)
		return
	}
	response := make([]deploymentResponse, 0, len(items))
	for _, item := range items {
		response = append(response, toDeploymentResponse(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h appHandler) getDeployment(w http.ResponseWriter, r *http.Request) {
	item, _, ok := h.deploymentForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, deploymentDetailResponse{
		deploymentResponse: toDeploymentResponse(item),
		Log:                item.Log,
	})
}

func (h appHandler) createDeployment(w http.ResponseWriter, r *http.Request) {
	app, err := h.app(r)
	if err != nil {
		appStoreError(w, err)
		return
	}
	var request deploymentRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	commit := strings.ToLower(strings.TrimSpace(request.Commit))
	if commit != "" && !deploymentCommitRe.MatchString(commit) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if h.deps.Queue == nil {
		writeError(w, http.StatusServiceUnavailable, "deployment_unavailable")
		return
	}
	if err := h.deps.Queue.Enqueue(r.Context(), app.ID, "manual", commit); err != nil {
		internal(w, "enqueue deployment", err)
		return
	}
	h.audit(r, "deployment_requested", map[string]any{
		"app_id": app.ID, "server_id": app.ServerID, "stack": app.StackName, "trigger": "manual", "commit": commit,
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (h appHandler) rollbackDeployment(w http.ResponseWriter, r *http.Request) {
	item, app, ok := h.deploymentForRequest(w, r)
	if !ok {
		return
	}
	if item.State != store.DeploymentSucceeded {
		writeError(w, http.StatusBadRequest, "deployment_not_succeeded")
		return
	}
	commit := strings.ToLower(strings.TrimSpace(item.CommitSHA))
	if !deploymentCommitRe.MatchString(commit) {
		writeError(w, http.StatusBadRequest, "deployment_has_no_commit")
		return
	}
	if h.deps.Queue == nil {
		writeError(w, http.StatusServiceUnavailable, "deployment_unavailable")
		return
	}
	if err := h.deps.Queue.Enqueue(r.Context(), app.ID, "rollback", commit); err != nil {
		internal(w, "enqueue rollback", err)
		return
	}
	h.audit(r, "deployment_rollback", map[string]any{
		"app_id": app.ID, "server_id": app.ServerID, "stack": app.StackName, "deployment_id": item.ID, "commit": commit,
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (h appHandler) deploymentForRequest(w http.ResponseWriter, r *http.Request) (store.Deployment, store.App, bool) {
	item, err := h.deps.Store.GetDeployment(r.Context(), chi.URLParam(r, "deploymentID"))
	if err != nil {
		appStoreError(w, err)
		return store.Deployment{}, store.App{}, false
	}
	app, err := h.deps.Store.GetApp(r.Context(), item.AppID)
	if err != nil {
		appStoreError(w, err)
		return store.Deployment{}, store.App{}, false
	}
	if appID := chi.URLParam(r, "appID"); appID != "" && appID != item.AppID {
		writeError(w, http.StatusNotFound, "not_found")
		return store.Deployment{}, store.App{}, false
	}
	return item, app, true
}

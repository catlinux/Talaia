package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/store"
)

type domainResponse struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	Hostname  string    `json:"hostname"`
	Service   string    `json:"service"`
	Port      int       `json:"port"`
	CreatedAt time.Time `json:"created_at"`
	Warning   string    `json:"warning,omitempty"`
}

type domainRequest struct {
	Hostname string `json:"hostname"`
	Service  string `json:"service"`
	Port     int    `json:"port"`
}

func NewDomainListHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.listDomains)
}

func NewDomainAddHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.addDomain)
}

func NewDomainDeleteHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.deleteDomain)
}

func (h appHandler) listDomains(w http.ResponseWriter, r *http.Request) {
	app, err := h.app(r)
	if err != nil {
		appStoreError(w, err)
		return
	}
	domains, err := h.deps.Store.ListAppDomains(r.Context(), app.ID)
	if err != nil {
		internal(w, "list app domains", err)
		return
	}
	response := make([]domainResponse, 0, len(domains))
	for _, domain := range domains {
		response = append(response, h.domainResponse(r, domain))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h appHandler) addDomain(w http.ResponseWriter, r *http.Request) {
	app, err := h.app(r)
	if err != nil {
		appStoreError(w, err)
		return
	}
	var request domainRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	hostname := strings.ToLower(strings.TrimSpace(request.Hostname))
	if !validDomainName(hostname) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	service := request.Service
	if service == "" {
		service = app.Service
	}
	if !appServiceRe.MatchString(service) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	port := request.Port
	if port == 0 {
		port = app.Port
	}
	if port < 1 || port > 65535 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	domain, err := h.deps.Store.AddDomain(r.Context(), store.Domain{
		AppID: app.ID, Hostname: hostname, Service: service, Port: port,
	})
	if errors.Is(err, store.ErrDomainExists) {
		writeError(w, http.StatusConflict, "domain_exists")
		return
	}
	if err != nil {
		internal(w, "add app domain", err)
		return
	}
	h.audit(r, "domain_added", map[string]any{
		"app_id": app.ID, "server_id": app.ServerID, "stack": app.StackName, "domain_id": domain.ID, "hostname": domain.Hostname,
	})
	writeJSON(w, http.StatusCreated, h.domainResponse(r, domain))
}

func (h appHandler) deleteDomain(w http.ResponseWriter, r *http.Request) {
	var app store.App
	appID := chi.URLParam(r, "appID")
	if appID != "" {
		var err error
		app, err = h.deps.Store.GetApp(r.Context(), appID)
		if err != nil {
			appStoreError(w, err)
			return
		}
	}
	domain, err := h.deps.Store.GetDomain(r.Context(), chi.URLParam(r, "domainID"))
	if err != nil {
		appStoreError(w, err)
		return
	}
	if appID == "" {
		app, err = h.deps.Store.GetApp(r.Context(), domain.AppID)
		if err != nil {
			appStoreError(w, err)
			return
		}
	}
	if domain.AppID != app.ID {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err := h.deps.Store.RemoveDomain(r.Context(), domain.ID); err != nil {
		appStoreError(w, err)
		return
	}
	h.audit(r, "domain_removed", map[string]any{
		"app_id": app.ID, "server_id": app.ServerID, "stack": app.StackName, "domain_id": domain.ID, "hostname": domain.Hostname,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h appHandler) domainResponse(r *http.Request, domain store.Domain) domainResponse {
	return domainResponse{
		ID: domain.ID, AppID: domain.AppID, Hostname: domain.Hostname,
		Service: domain.Service, Port: domain.Port, CreatedAt: domain.CreatedAt,
		Warning: h.domainWarning(r, domain.Hostname),
	}
}

func (h appHandler) domainWarning(r *http.Request, hostname string) string {
	if h.deps.Resolver == nil {
		return ""
	}
	addresses, err := h.deps.Resolver.LookupHost(r.Context(), hostname)
	if err != nil || len(addresses) == 0 {
		return "the hostname does not resolve"
	}
	agent, ok := connectedAgent(h.deps.Agents, chi.URLParam(r, "serverID"))
	if !ok {
		app, err := h.app(r)
		if err != nil {
			return ""
		}
		agent, ok = connectedAgent(h.deps.Agents, app.ServerID)
	}
	if !ok || strings.TrimSpace(agent.Hostname) == "" {
		return ""
	}
	serverAddresses, err := h.deps.Resolver.LookupHost(r.Context(), agent.Hostname)
	if err != nil || len(serverAddresses) == 0 {
		return ""
	}
	known := make(map[string]bool, len(serverAddresses))
	for _, address := range serverAddresses {
		known[address] = true
	}
	for _, address := range addresses {
		if known[address] {
			return ""
		}
	}
	return "the hostname does not resolve to the server"
}

func validDomainName(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 253 || strings.Contains(hostname, "*") {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

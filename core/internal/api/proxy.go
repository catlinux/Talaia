package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// ProxyDeps are the collaborators of the proxy endpoints.
type ProxyDeps struct {
	AppDeps
	Containers *ContainerDeps
}

type proxyResponse struct {
	Mode    string        `json:"mode"`
	Domains []proxyDomain `json:"domains"`
}

type proxyDomain struct {
	Hostname string `json:"hostname"`
	AppID    string `json:"app_id"`
	HostPort int    `json:"host_port"`
	Warning  string `json:"warning,omitempty"`
	Apache   string `json:"apache,omitempty"`
	Nginx    string `json:"nginx,omitempty"`
	Certbot  string `json:"certbot,omitempty"`
}

type proxyActionRequest struct {
	Action string `json:"action"`
}

func NewProxyHandler(deps ProxyDeps) http.Handler {
	h := appHandler{deps: deps.AppDeps}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")
		agent, connected := connectedAgent(deps.Agents, serverID)
		mode := agent.ProxyMode
		response := proxyResponse{Mode: proxyModeName(mode), Domains: []proxyDomain{}}
		if !connected || mode == agentv1.ProxyMode_PROXY_MODE_MANAGED {
			writeJSON(w, http.StatusOK, response)
			return
		}

		domains, err := deps.Store.ListDomains(r.Context())
		if err != nil {
			internal(w, "list proxy domains", err)
			return
		}
		apps, err := deps.Store.ListApps(r.Context())
		if err != nil {
			internal(w, "list proxy apps", err)
			return
		}
		byID := make(map[string]store.App, len(apps))
		for _, app := range apps {
			byID[app.ID] = app
		}
		for _, domain := range domains {
			app, ok := byID[domain.AppID]
			if !ok || app.ServerID != serverID {
				continue
			}
			item := proxyDomain{
				Hostname: domain.Hostname, AppID: app.ID, HostPort: app.HostPort,
				Warning: h.proxyDomainWarning(r, agent.Hostname, domain.Hostname),
			}
			if app.HostPort > 0 {
				item.Apache = apacheBlock(domain.Hostname, app.HostPort)
				item.Nginx = nginxBlock(domain.Hostname, app.HostPort)
				item.Certbot = certbotSteps(domain.Hostname)
			} else if item.Warning == "" {
				item.Warning = "the app has no host port yet"
			}
			response.Domains = append(response.Domains, item)
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func NewProxyActionHandler(deps ProxyDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")
		var request proxyActionRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		action := strings.ToLower(strings.TrimSpace(request.Action))
		if action != "install" && action != "remove" && action != "apply" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if deps.Containers == nil {
			writeError(w, http.StatusServiceUnavailable, "proxy_unavailable")
			return
		}

		if action == "apply" {
			agent, _ := connectedAgent(deps.Agents, serverID)
			if agent.ProxyMode != agentv1.ProxyMode_PROXY_MODE_MANAGED {
				writeError(w, http.StatusBadRequest, "proxy_not_managed")
				return
			}
			routes, err := proxyRoutes(r, deps.Store, serverID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request")
				return
			}
			submitJob(w, r, *deps.Containers, serverID, proxyApplyKind, proxyApplyKind,
				map[string]any{"action": action}, map[string]any{"action": action},
				func(jobID string) *agentv1.CoreMsg {
					return &agentv1.CoreMsg{
						JobId: jobID,
						Cmd:   &agentv1.CoreMsg_ProxyApply{ProxyApply: &agentv1.ProxyApply{Routes: routes}},
					}
				})
			return
		}

		installAction := agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL
		if action == "remove" {
			installAction = agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_REMOVE
		}
		submitJob(w, r, *deps.Containers, serverID, proxyInstallKind, proxyInstallKind,
			map[string]any{"action": action}, map[string]any{"action": action},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_ProxyInstall{ProxyInstall: &agentv1.ProxyInstall{
						Action: installAction,
					}},
				}
			})
	})
}

func proxyRoutes(r *http.Request, state AppStore, serverID string) ([]*agentv1.ProxyRoute, error) {
	domains, err := state.ListDomains(r.Context())
	if err != nil {
		return nil, err
	}
	apps, err := state.ListApps(r.Context())
	if err != nil {
		return nil, err
	}
	byID := make(map[string]store.App, len(apps))
	for _, app := range apps {
		byID[app.ID] = app
	}
	routes := make([]*agentv1.ProxyRoute, 0, len(domains))
	for _, domain := range domains {
		app, ok := byID[domain.AppID]
		if !ok || app.ServerID != serverID {
			continue
		}
		service := domain.Service
		if service == "" {
			service = app.Service
		}
		port := domain.Port
		if port == 0 {
			port = app.Port
		}
		if port <= 0 || port > 65535 || service == "" {
			return nil, errors.New("the app has no valid service port")
		}
		routes = append(routes, &agentv1.ProxyRoute{
			Hostname: domain.Hostname, UpstreamService: service, UpstreamPort: uint32(port),
		})
	}
	return routes, nil
}

func proxyModeName(mode agentv1.ProxyMode) string {
	switch mode {
	case agentv1.ProxyMode_PROXY_MODE_MANAGED:
		return "managed"
	default:
		return "external"
	}
}

func (h appHandler) proxyDomainWarning(r *http.Request, serverHostname, hostname string) string {
	if h.deps.Resolver == nil {
		return ""
	}
	addresses, err := h.deps.Resolver.LookupHost(r.Context(), hostname)
	if err != nil || len(addresses) == 0 {
		return "the hostname does not resolve"
	}
	if strings.TrimSpace(serverHostname) == "" {
		return ""
	}
	serverAddresses, err := h.deps.Resolver.LookupHost(r.Context(), serverHostname)
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

// apacheBlock is the site for port 80 only: `certbot --apache` then writes the
// HTTPS copy itself, so the block never names a certificate that does not exist
// yet (apachectl configtest would fail). mod_proxy already adds X-Forwarded-For,
// X-Forwarded-Host and X-Forwarded-Server.
func apacheBlock(hostname string, port int) string {
	return fmt.Sprintf(`<VirtualHost *:80>
    ServerName %[1]s
    ProxyPreserveHost On
    ProxyPass / http://127.0.0.1:%[2]d/
    ProxyPassReverse / http://127.0.0.1:%[2]d/
    RequestHeader set X-Forwarded-Proto expr=%%{REQUEST_SCHEME}
    RewriteEngine On
    RewriteCond %%{HTTP:Upgrade} =websocket [NC]
    RewriteRule /(.*) ws://127.0.0.1:%[2]d/$1 [P,L]
</VirtualHost>
`, hostname, port)
}

// nginxBlock is the site for port 80 only; `certbot --nginx` adds HTTPS.
func nginxBlock(hostname string, port int) string {
	return fmt.Sprintf(`server {
    listen 80;
    server_name %[1]s;
    location / {
        proxy_pass http://127.0.0.1:%[2]d;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
`, hostname, port)
}

func certbotSteps(hostname string) string {
	return fmt.Sprintf(`# Apache: save the block as /etc/apache2/sites-available/%[1]s.conf, then
sudo a2enmod proxy proxy_http proxy_wstunnel headers rewrite
sudo a2ensite %[1]s
sudo apachectl configtest && sudo systemctl reload apache2
sudo certbot --apache -d %[1]s

# nginx: save the block as /etc/nginx/sites-available/%[1]s, then
sudo ln -s /etc/nginx/sites-available/%[1]s /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d %[1]s
`, hostname)
}

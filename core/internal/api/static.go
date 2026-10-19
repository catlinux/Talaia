package api

import (
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

// reservedExact are the routes of the API, the agent protocol and the health
// check that the panel must never shadow.
var reservedExact = []string{
	"/healthz",
	"/enroll",
	"/renew",
	"/ca.pem",
	"/install.sh",
	agentv1connect.AgentHubConnectProcedure,
}

// Static serves the compiled panel from dir. A path that is not a file is a
// client-side route and gets index.html.
func Static(dir string) http.Handler {
	return &staticHandler{dir: dir}
}

type staticHandler struct {
	dir     string
	once    sync.Once
	missing bool
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if reserved(r.URL.Path) || strings.Contains(r.URL.Path, "..") {
		http.NotFound(w, r)
		return
	}
	if !h.available() {
		http.NotFound(w, r)
		return
	}

	// The path is cleaned before joining, so it can only point inside the panel
	// directory.
	filePath := filepath.Join(h.dir, filepath.FromSlash(path.Clean("/"+r.URL.Path)))
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, filePath)
		return
	}
	http.ServeFile(w, r, filepath.Join(h.dir, "index.html"))
}

// available reports whether the panel directory exists, logging once when it
// does not.
func (h *staticHandler) available() bool {
	h.once.Do(func() {
		info, err := os.Stat(h.dir)
		if err != nil || !info.IsDir() {
			slog.Warn("the web panel directory does not exist", "web_dir", h.dir)
			h.missing = true
		}
	})
	return !h.missing
}

// reserved reports whether a path belongs to the API or the agent protocol.
func reserved(requestPath string) bool {
	if strings.HasPrefix(requestPath, "/api/") || strings.HasPrefix(requestPath, "/agent/") {
		return true
	}
	for _, exact := range reservedExact {
		if requestPath == exact {
			return true
		}
	}
	return false
}

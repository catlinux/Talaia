package api

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

// agentPackages are the only files served under /agent/. The names are fixed so
// that a request can never select another path in the directory.
var agentPackages = map[string]bool{
	"talaia-agent_amd64.deb": true,
	"talaia-agent_arm64.deb": true,
}

// NewDistHandler serves GET /install.sh and GET /agent/<package>.deb from dir, so
// a new server can install its agent from the core. Both files are public: they
// carry no secret, and install.sh verifies the CA fingerprint before it trusts
// the package. An empty dir, or a missing file, answers 404.
func NewDistHandler(dir string) http.Handler {
	serve := func(w http.ResponseWriter, r *http.Request, name, contentType string) {
		if dir == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, filepath.Join(dir, name))
	}

	r := chi.NewRouter()
	r.Get("/install.sh", func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, "install.sh", "text/x-shellscript; charset=utf-8")
	})
	r.Get("/agent/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if !agentPackages[name] {
			http.NotFound(w, r)
			return
		}
		serve(w, r, name, "application/vnd.debian.binary-package")
	})
	return r
}

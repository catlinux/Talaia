package api

import (
	"mime"
	"net/http"
	"strings"
)

// contentSecurityPolicy is the policy of the panel: no inline or third-party
// code, no framing, and forms only post to the panel itself.
const contentSecurityPolicy = "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// SecurityConfig are the settings of the security middleware.
type SecurityConfig struct {
	// TLS enables HSTS, which only makes sense over HTTPS.
	TLS bool
	// Origins are the browser origins the panel may be served from, as
	// scheme://host[:port].
	Origins []string
}

// Security adds the security headers to every response and rejects state
// changing API requests that do not come from the panel.
func Security(config SecurityConfig) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(config.Origins))
	for _, origin := range config.Origins {
		allowed[strings.ToLower(origin)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			header.Set("Content-Security-Policy", contentSecurityPolicy)
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("Referrer-Policy", "no-referrer")
			if config.TLS {
				header.Set("Strict-Transport-Security", "max-age=31536000")
			}

			apiRequest := strings.HasPrefix(r.URL.Path, "/api/")
			if apiRequest {
				header.Set("Cache-Control", "no-store")
			}

			if apiRequest && stateChanging(r.Method) && !allowedChange(r, allowed) {
				writeError(w, http.StatusForbidden, "forbidden_origin")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// stateChanging reports whether a method can change state.
func stateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// allowedChange reports whether a state changing request comes from the panel.
// It must declare a JSON body and, when the browser sends an Origin, that
// origin must be allowed; a browser that only sends Sec-Fetch-Site must mark the
// request as same-origin.
func allowedChange(r *http.Request, allowed map[string]bool) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return false
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		return allowed[strings.ToLower(origin)]
	}
	return strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "same-origin")
}

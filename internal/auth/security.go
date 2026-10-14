package auth

// Adapted from Switch Library Manager Web by the same author.

import (
	"errors"
	"net/http"
	"net/url"
	"os"
)

// sameOriginOnly rejects state changing requests sent by other web sites (CSRF): without it
// any page open in the browser could start a synchronization or reorganize the library.
// Requests without browser headers (curl, scripts) are allowed.
func SameOriginOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		if !isSameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-site request rejected")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isSameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			return false
		}
	}

	return true
}

// authFromEnv returns the credentials set with COMPLETIST_AUTH_USERNAME and COMPLETIST_AUTH_PASSWORD.
// Authentication is disabled when neither is set; setting only one is an error.
func authFromEnv() (username string, password string, enabled bool, err error) {
	username, password = os.Getenv("COMPLETIST_AUTH_USERNAME"), os.Getenv("COMPLETIST_AUTH_PASSWORD")
	if username == "" && password == "" {
		return "", "", false, nil
	}
	if username == "" || password == "" {
		return "", "", false, errors.New("both COMPLETIST_AUTH_USERNAME and COMPLETIST_AUTH_PASSWORD must be set to enable authentication")
	}
	return username, password, true, nil
}

// weakEnvPassword reports why the password of COMPLETIST_AUTH_PASSWORD is weak, or nil. A weak one
// still works, so nobody is locked out of the app after an update: the log and the pages of the
// administrators ask to change it.
func weakEnvPassword(username, password string) error {
	if username == "" || password == "" {
		return nil
	}
	return checkPassword(username, password)
}

// contentSecurityPolicy allows only the app's own scripts, styles and fonts, plus the inline
// scripts of the interface named by their hashes. Images may also come from the servers of the
// cover sources.
func contentSecurityPolicy(scriptHashes []string) string {
	scripts := "'self'"
	for _, h := range scriptHashes {
		scripts += " '" + h + "'"
	}
	return "default-src 'self'; script-src " + scripts + "; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: https:; font-src 'self'; connect-src 'self'; worker-src 'self'; manifest-src 'self'; " +
		"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"
}

// withSecurityHeaders adds headers that make the browser refuse foreign scripts, framing
// by other sites and content type guessing.
func WithSecurityHeaders(next http.Handler, scriptHashes []string) http.Handler {
	policy := contentSecurityPolicy(scriptHashes)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", policy)
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "same-origin")
		headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		headers.Set("Cross-Origin-Resource-Policy", "same-origin")
		// served over https (directly or through a proxy): browsers keep using https
		if isHttps(r) {
			headers.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

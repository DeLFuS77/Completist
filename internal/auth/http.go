package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// New loads the users and the session key from the data folder.
func New(dataFolder string) (*Auth, error) {
	return newAuth(dataFolder)
}

// Middleware requires a login when users exist and limits read-only users.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return a.middleware(next)
}

// PrincipalFrom returns who sent the request.
func PrincipalFrom(r *http.Request) *Principal {
	return principalFrom(r)
}

// WeakEnvPassword reports why the password set in the environment is weak, or nil.
func (a *Auth) WeakEnvPassword() error {
	return a.envPasswordWeak
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// Status is what the interface knows about the login.
type Status struct {
	Enabled         bool   `json:"enabled"`
	User            string `json:"user"`
	IsAdmin         bool   `json:"isAdmin"`
	FromEnv         bool   `json:"fromEnv"`
	Language        string `json:"language"`
	WeakEnvPassword bool   `json:"weakEnvPassword"`
	LoggedIn        bool   `json:"loggedIn"`
}

func (a *Auth) status(r *http.Request) Status {
	principal := a.fromSession(r)
	st := Status{Enabled: a.Enabled()}
	if !st.Enabled {
		// without users everybody on the local network is an administrator
		st.IsAdmin, st.LoggedIn = true, true
		return st
	}
	if principal != nil {
		st.User, st.IsAdmin, st.FromEnv, st.LoggedIn = principal.Name, principal.IsAdmin(), principal.Source == "env", true
		if user, ok := a.users.Get(principal.Name); ok {
			st.Language = user.Language
		}
		st.WeakEnvPassword = principal.IsAdmin() && a.envPasswordWeak != nil
	}
	return st
}

func decode(r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64*1024)
	return json.NewDecoder(r.Body).Decode(value)
}

// userError maps the errors of the user store to responses; their texts are translated by the
// interface.
func userError(w http.ResponseWriter, err error) {
	for _, known := range userErrors {
		if errors.Is(err, known) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// Routes adds the login, account and user management endpoints.
func (a *Auth) Routes(mux *http.ServeMux, log *slog.Logger) {
	mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.status(r))
	})

	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		ip := clientIp(r)
		keys := loginKeys(ip, body.Name)
		retryAfter, delay := a.limiter.check(keys...)
		if retryAfter > 0 {
			minutes := int(retryAfter.Minutes()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": fmt.Sprintf(errTooManyLogins.Error(), minutes), "minutes": minutes})
			return
		}
		if delay > 0 {
			sleepFor(delay)
		}
		principal, ok := a.verify(body.Name, body.Password)
		if !ok {
			a.limiter.fail(keys...)
			log.Warn("failed login", "user", body.Name, "address", ip)
			writeError(w, http.StatusUnauthorized, "Wrong user name or password.")
			return
		}
		a.limiter.succeed(keys...)
		http.SetCookie(w, a.sessionCookie(r, principal.Name))
		log.Info("login", "user", principal.Name, "address", ip)
		r.AddCookie(a.sessionCookie(r, principal.Name))
		writeJSON(w, http.StatusOK, a.status(r))
	})

	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		a.endSession(r)
		http.SetCookie(w, clearSessionCookie(r))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/users", func(w http.ResponseWriter, r *http.Request) {
		type userView struct {
			Name     string    `json:"name"`
			Role     string    `json:"role"`
			Language string    `json:"language"`
			Created  time.Time `json:"created"`
		}
		out := []userView{}
		for _, u := range a.users.List() {
			out = append(out, userView{u.Name, u.Role, u.Language, u.Created})
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": out, "envUser": a.envUser})
	})

	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		first := !a.Enabled()
		if first {
			// the first account administers the app
			body.Role = ROLE_ADMIN
		}
		if err := a.users.add(body.Name, body.Password, body.Role, first); err != nil {
			userError(w, err)
			return
		}
		log.Info("user created", "user", body.Name, "role", body.Role)
		if first {
			// whoever created the first account is logged in with it
			http.SetCookie(w, a.sessionCookie(r, body.Name))
		}
		writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
	})

	mux.HandleFunc("PUT /api/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var body struct {
			Role     string `json:"role"`
			Password string `json:"password"`
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		if body.Role != "" {
			if err := a.users.SetRole(name, body.Role); err != nil {
				userError(w, err)
				return
			}
		}
		if body.Password != "" {
			if err := a.users.SetPassword(name, body.Password); err != nil {
				userError(w, err)
				return
			}
		}
		a.forget()
		// changing your own password ends your session too: start a new one
		if p := principalFrom(r); p != nil && p.Name == name && body.Password != "" {
			http.SetCookie(w, a.sessionCookie(r, name))
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("DELETE /api/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if p := principalFrom(r); p != nil && p.Name == name {
			userError(w, ErrDeleteSelf)
			return
		}
		if err := a.users.Delete(name); err != nil {
			userError(w, err)
			return
		}
		a.forget()
		log.Info("user deleted", "user", name)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/account/password", func(w http.ResponseWriter, r *http.Request) {
		principal := principalFrom(r)
		if principal == nil || principal.Name == "" || principal.Source == "env" {
			userError(w, ErrEnvironmentUser)
			return
		}
		var body struct {
			Current  string `json:"current"`
			Password string `json:"password"`
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		if _, ok := a.users.Verify(principal.Name, body.Current); !ok {
			userError(w, ErrWrongPassword)
			return
		}
		if err := a.users.SetPassword(principal.Name, body.Password); err != nil {
			userError(w, err)
			return
		}
		a.forget()
		http.SetCookie(w, a.sessionCookie(r, principal.Name))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/account/language", func(w http.ResponseWriter, r *http.Request) {
		principal := principalFrom(r)
		var body struct {
			Language string `json:"language"`
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		switch body.Language {
		case "", "en", "es":
		default:
			writeError(w, http.StatusBadRequest, "unknown language")
			return
		}
		if principal != nil && principal.Name != "" && principal.Source != "env" {
			if err := a.users.SetLanguage(principal.Name, body.Language); err != nil {
				userError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
}

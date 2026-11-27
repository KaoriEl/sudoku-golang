package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	authCookie   = "sudoku_session"
	cookieMaxAge = 30 * 24 * time.Hour
)

const authPage = `<!doctype html><meta charset="utf-8"><title>sudoku · вход</title>
<body style="font-family:system-ui,sans-serif;background:#0b0d10;color:#e7e9ee;padding:48px;max-width:640px">
<h1 style="font-size:20px">Нужен токен</h1>
<p>Админка защищена токеном (ADMIN_TOKEN). Откройте ссылку, которую <code>sudoku serve</code> напечатал при старте:
<code>http://…/?token=…</code></p></body>`

// sessionValue is what the cookie holds: a hash of the token, so the token itself never sits in the
// browser's cookie jar.
func sessionValue(token string) string {
	sum := sha256.Sum256([]byte("sudoku-session:" + token))
	return hex.EncodeToString(sum[:])
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// auth enforces ADMIN_TOKEN when it is set. The operator opens /?token=… once (the URL printed by
// `sudoku serve`); the server answers with an HttpOnly SameSite=Strict cookie and redirects to the
// clean URL. API clients may send "Authorization: Bearer <token>" instead. Without a token configured
// the panel relies on loopback-only binding and the CSRF guard, as before.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.svc.authToken
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		want := sessionValue(token)
		if q := r.URL.Query().Get("token"); q != "" && r.Method == http.MethodGet {
			if !equal(q, token) {
				s.unauthorized(w, r)
				return
			}
			// Secure only with TLS: the panel is plain HTTP on loopback, where browsers would drop a Secure cookie.
			http.SetCookie(w, &http.Cookie{ //nolint:gosec // HttpOnly + SameSite=Strict; Secure would break plain-HTTP loopback
				Name: authCookie, Value: want, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
				MaxAge: int(cookieMaxAge.Seconds()), Secure: r.TLS != nil,
			})
			http.Redirect(w, r, localRedirect(r), http.StatusFound) //nolint:gosec // localRedirect only yields a same-origin path
			return
		}
		if c, err := r.Cookie(authCookie); err == nil && equal(c.Value, want) {
			next.ServeHTTP(w, r)
			return
		}
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && equal(strings.TrimSpace(bearer), token) {
			next.ServeHTTP(w, r)
			return
		}
		s.unauthorized(w, r)
	})
}

// localRedirect is the request path without the token parameter. Leading slashes are collapsed so
// "//evil.example/?token=…" cannot turn into a protocol-relative redirect to another host.
func localRedirect(r *http.Request) string {
	q := r.URL.Query()
	q.Del("token")
	target := "/" + strings.TrimLeft(r.URL.Path, "/\\")
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	return target
}

func (s *Server) unauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusUnauthorized, apiError{Error: "нужен токен (ADMIN_TOKEN)", Code: "unauthorized"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(authPage))
}

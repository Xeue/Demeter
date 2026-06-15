package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/Xeue/Demeter/internal/store"
)

const sessionCookie = "demeter_session"

type ctxKey int

const sessionCtxKey ctxKey = 0

func tokenString(nbytes int) string {
	b := make([]byte, nbytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// CreateSession mints and stores a session for a user.
func (a *Auth) CreateSession(u *User) *Session {
	s := &Session{
		Token:    tokenString(32),
		Username: u.Username,
		Role:     u.Role,
		Expiry:   a.now().Add(a.sessionTTL),
	}
	a.mu.Lock()
	a.sessions[s.Token] = s
	a.mu.Unlock()
	a.persistSessions()
	return s
}

// Validate returns the session for a token if present and unexpired, extending
// its expiry (sliding window).
func (a *Auth) Validate(token string) (*Session, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[token]
	if s == nil {
		return nil, false
	}
	if a.now().After(s.Expiry) {
		delete(a.sessions, token)
		return nil, false
	}
	s.Expiry = a.now().Add(a.sessionTTL)
	cp := *s
	return &cp, true
}

// DeleteSession removes a session (logout).
func (a *Auth) DeleteSession(token string) {
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
	a.persistSessions()
}

func (a *Auth) persistSessions() {
	a.mu.Lock()
	snap := make(map[string]*Session, len(a.sessions))
	for k, v := range a.sessions {
		cp := *v
		snap[k] = &cp
	}
	a.mu.Unlock()
	_ = store.WriteJSON(a.sessionsPath(), snap)
}

// SetCookie writes the session cookie.
func (a *Auth) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secure,
		Expires:  a.now().Add(a.sessionTTL),
	})
}

// ClearCookie removes the session cookie.
func (a *Auth) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
	})
}

// SessionFromRequest validates the request's session cookie.
func (a *Auth) SessionFromRequest(r *http.Request) (*Session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, false
	}
	return a.Validate(c.Value)
}

// CookieName returns the session cookie name (for logout handlers).
func (a *Auth) CookieName() string { return sessionCookie }

// PageMiddleware redirects unauthenticated requests to /login; otherwise it
// attaches the session to the request context.
func (a *Auth) PageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.SessionFromRequest(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionCtxKey, s)))
	})
}

// SessionFromContext returns the session attached by PageMiddleware.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionCtxKey).(*Session)
	return s, ok
}

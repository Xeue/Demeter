// Package web hosts the existing Demeter GUI over HTTP + WebSocket, replacing
// Electron. It serves the embedded static assets and page template, handles
// login/logout + sessions, and gates the WebSocket upgrade behind auth.
package web

import (
	"context"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"

	demeter "github.com/Xeue/Demeter"
	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/config"
	"github.com/Xeue/Demeter/internal/hub"
	"github.com/coder/websocket"
)

// Server is the HTTP server.
type Server struct {
	cfg     config.Config
	version string
	auth    *auth.Auth
	hub     *hub.Hub
	tmpl    *template.Template
}

// NewServer parses the embedded templates and wires routes.
func NewServer(cfg config.Config, version string, a *auth.Auth, h *hub.Hub) (*Server, error) {
	tmpl, err := template.ParseFS(demeter.ViewsFS, "views/*.gohtml")
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, version: version, auth: a, hub: h, tmpl: tmpl}, nil
}

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServerFS(demeter.StaticFS))
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/desktop-login", s.handleDesktopLogin)
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", s.auth.PageMiddleware(http.HandlerFunc(s.handleIndex)))
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.renderApp(w, r)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.renderLogin(w, "")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			s.renderLogin(w, "Invalid form")
			return
		}
		user, err := s.auth.Login(r.FormValue("username"), r.FormValue("password"))
		if err != nil {
			s.auth.Audit().Log(strings.ToLower(r.FormValue("username")), "", "loginFailed", nil, clientIP(r))
			s.renderLogin(w, "Login failed")
			return
		}
		sess := s.auth.CreateSession(user)
		s.auth.SetCookie(w, sess.Token)
		s.auth.Audit().Log(user.Username, user.Role, "login", nil, clientIP(r))
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.auth.CookieName()); err == nil {
		if sess, ok := s.auth.Validate(c.Value); ok {
			s.auth.Audit().Log(sess.Username, sess.Role, "logout", nil, clientIP(r))
		}
		s.auth.DeleteSession(c.Value)
	}
	s.auth.ClearCookie(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// handleDesktopLogin lets the bundled desktop window auto-authenticate over
// loopback: it accepts a freshly-minted session token in the query, sets the
// cookie, and redirects to the app. It only honours loopback requests, and the
// token must already be a valid session, so it adds no attack surface beyond
// "you already have the cookie".
func (s *Server) handleDesktopLogin(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) {
		http.NotFound(w, r)
		return
	}
	token := r.URL.Query().Get("token")
	if _, ok := s.auth.Validate(token); !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	s.auth.SetCookie(w, token)
	http.Redirect(w, r, "/", http.StatusFound)
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.auth.SessionFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	// Serve blocks until the client disconnects.
	s.hub.Serve(r.Context(), conn, sess, clientIP(r))
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) newHTTPServer(ctx context.Context) *http.Server {
	srv := &http.Server{Addr: s.cfg.ListenAddr, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	return srv
}

// ListenAndServe runs the server until ctx is cancelled, honouring TLS config.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := s.newHTTPServer(ctx)
	if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		return srv.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
	}
	return srv.ListenAndServe()
}

// Serve runs the server on a caller-supplied listener (used by the desktop
// binary, which binds a loopback port first to learn the URL).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := s.newHTTPServer(ctx)
	if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		return srv.ServeTLS(ln, s.cfg.TLSCert, s.cfg.TLSKey)
	}
	return srv.Serve(ln)
}

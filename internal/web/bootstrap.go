package web

import (
	"html/template"
	"net/http"

	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/commandsdb"
)

type pageData struct {
	Static            string
	Background        string
	SystemName        string
	Version           string
	Username          string
	IsAdmin           bool
	AutoRebootDefault bool
	Commands          template.JS
}

func (s *Server) renderApp(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.SessionFromContext(r.Context())
	data := pageData{
		Static:            "/static",
		Background:        "bg-dark", // mica is Electron/Windows-only; headless is always dark
		SystemName:        s.cfg.SystemName,
		Version:           s.version,
		AutoRebootDefault: s.hub.GlobalAutoReboot(),
		Commands:          template.JS(commandsdb.RawJSON()),
	}
	if sess != nil {
		data.Username = sess.Username
		data.IsAdmin = sess.Role == auth.RoleAdmin
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "app.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tmpl.ExecuteTemplate(w, "login.gohtml", map[string]any{
		"SystemName": s.cfg.SystemName,
		"Error":      errMsg,
		"Static":     "/static",
		"Version":    s.version,
	})
}

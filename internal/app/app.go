// Package app wires the Demeter server together and is shared by both the
// headless server binary (cmd/demeter) and the desktop webview binary
// (cmd/demeter-desktop). It owns construction, lifecycle and a desktop
// auto-login helper, but not flag parsing (the binaries do that).
package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	demeter "github.com/Xeue/Demeter"
	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/config"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/hub"
	"github.com/Xeue/Demeter/internal/logging"
	"github.com/Xeue/Demeter/internal/manager"
	"github.com/Xeue/Demeter/internal/pool"
	"github.com/Xeue/Demeter/internal/scan"
	"github.com/Xeue/Demeter/internal/store"
	"github.com/Xeue/Demeter/internal/web"
)

// App holds the constructed (but not yet running) server components.
type App struct {
	Cfg   config.Config
	Store *store.Store
	Auth  *auth.Auth
	Audit *auth.Audit
	Hub   *hub.Hub
	Mgr   *manager.Manager
	Srv   *web.Server

	log *logging.Handler
}

// Build constructs the full stack: logging, persistence, auth+audit, the scan
// engine and the web server. It does not start goroutines (Run does) or create
// the bootstrap admin (call Bootstrap).
func Build(ctx context.Context, cfg config.Config, workers int) (*App, error) {
	// Logging.
	logOut := io.Writer(os.Stderr)
	if cfg.CreateLogFile {
		if f, err := openLogFile(cfg.DataDir); err == nil {
			logOut = io.MultiWriter(os.Stderr, f)
		}
	}
	logHandler := logging.New(logOut, logging.LevelFor(cfg.LoggingLevel), time.Now)
	slog.SetDefault(slog.New(logHandler))

	st, err := store.New(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	audit := auth.NewAudit(filepath.Join(cfg.DataDir, "logs"), time.Now)
	a, err := auth.New(cfg.DataDir, audit, cfg.TLSCert != "", time.Now)
	if err != nil {
		return nil, err
	}
	db, err := commandsdb.Load()
	if err != nil {
		return nil, err
	}

	h := hub.New(a)
	logHandler.SetEmitter(h.Log)
	scanner := &scan.Scanner{DB: db, Pool: pool.New(workers), Events: h}
	autoOpts := manager.AutoRebootOptions{
		Default:  cfg.AutoReboot,
		Cooldown: cfg.AutoRebootCooldown(),
		Audit: func(action string, detail any) {
			audit.Log("system", auth.RoleAdmin, action, detail, "")
		},
		Persist: func(enabled bool) {
			cfg.AutoReboot = enabled
			if err := cfg.Save(); err != nil {
				slog.Warn("persist auto-reboot default failed", "err", err)
			}
		},
	}
	dialer := device.RollcallDialer{
		Mode:          device.ParseMode(cfg.RollcallMode),
		SetOpcode:     device.ParseSetOpcode(cfg.RollcallSetOpcode),
		Port:          cfg.RollcallPort,
		Handshake:     cfg.RollcallHandshake,
		PerGetTimeout: cfg.RollcallTimeout(),
	}
	slog.Info("rollcall connection", "mode", cfg.RollcallMode, "port", cfg.RollcallPort, "setOpcode", cfg.RollcallSetOpcode, "handshake", cfg.RollcallHandshake, "getTimeoutMs", cfg.RollcallTimeoutMs)
	mgr := manager.New(ctx, scanner, dialer, st, h, st.Frames(), st.Groups(), cfg.ScanInterval(), autoOpts)
	mgr.SetIntervalPersister(func(seconds int) {
		cfg.ScanIntervalSeconds = seconds
		if err := cfg.Save(); err != nil {
			slog.Warn("persist scan interval failed", "err", err)
		}
	})
	h.SetEngine(mgr)

	srv, err := web.NewServer(cfg, version(), a, h)
	if err != nil {
		return nil, err
	}

	return &App{Cfg: cfg, Store: st, Auth: a, Audit: audit, Hub: h, Mgr: mgr, Srv: srv, log: logHandler}, nil
}

// Bootstrap creates the initial admin if none exists.
func (a *App) Bootstrap() error { return a.Auth.Bootstrap() }

// Version returns the build version.
func (a *App) Version() string { return version() }

// startBackground launches the persistence, hub and poll-loop goroutines.
func (a *App) startBackground(ctx context.Context) {
	go a.Store.Run(ctx)
	go a.Hub.Run(ctx.Done())
	a.Mgr.Start()
}

// Run starts the background goroutines and serves on the configured address
// until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	a.startBackground(ctx)
	slog.Info("Demeter started", "version", version(), "listen", a.Cfg.ListenAddr, "data", a.Cfg.DataDir)
	return a.Srv.ListenAndServe(ctx)
}

// RunListener is like Run but serves on a caller-supplied listener (used by the
// desktop binary, which binds a loopback port first to learn the URL).
func (a *App) RunListener(ctx context.Context, ln net.Listener) error {
	a.startBackground(ctx)
	slog.Info("Demeter started (desktop)", "version", version(), "addr", ln.Addr().String(), "data", a.Cfg.DataDir)
	return a.Srv.Serve(ctx, ln)
}

// Shutdown flushes state and the audit log.
func (a *App) Shutdown() {
	a.Store.Close()
	a.Audit.Close()
	slog.Info("Demeter stopped")
}

// DesktopSession ensures an admin user exists and returns a fresh session token,
// so the desktop window can auto-authenticate over loopback without a login
// prompt. If it has to create the account, the generated password is recorded as
// a one-time notice so the GUI can show it (the desktop user has no terminal).
// Auto-login mints the session server-side, so it keeps working after the user
// changes the password.
func (a *App) DesktopSession(username string) (string, error) {
	if username == "" {
		username = "admin"
	}
	if a.Auth.User(username) == nil {
		pw := randomPassword()
		if err := a.Auth.CreateUser(username, pw, auth.RoleAdmin); err != nil {
			return "", err
		}
		a.Auth.SetNotice(username, pw)
	}
	u := a.Auth.User(username)
	if u == nil {
		return "", os.ErrInvalid
	}
	return a.Auth.CreateSession(u).Token, nil
}

func randomPassword() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func openLogFile(dataDir string) (*os.File, error) {
	dir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "Demeter.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

// versionOverride is stamped at build time via
// -ldflags "-X github.com/Xeue/Demeter/internal/app.versionOverride=X" (the
// Makefile does this from the VERSION file). When unset, the version falls back
// to the embedded VERSION file, so a plain `go build` still reports the right value.
var versionOverride string

// Version returns the app version (for the GUI, logs and `--version`).
func Version() string { return version() }

func version() string {
	if versionOverride != "" {
		return versionOverride
	}
	if v := strings.TrimSpace(demeter.VersionFile); v != "" {
		return v
	}
	return "dev"
}

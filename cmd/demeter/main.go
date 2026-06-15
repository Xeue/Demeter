// Command demeter is the headless Go server: it speaks RollCall to frames (via
// the rollcall package, behind the device adapter), runs the scan/blast engine,
// and hosts the existing web GUI over HTTP + WebSocket.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Xeue/Demeter/internal/app"
	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/config"
)

func main() {
	var (
		listen      = flag.String("listen", "", "listen address (overrides config, default :8080)")
		dataDir     = flag.String("data-dir", defaultDataDir(), "data directory")
		logLevel    = flag.String("log-level", "", "log level A|D|W|E (overrides config)")
		tlsCert     = flag.String("tls-cert", "", "TLS certificate file (enables HTTPS/wss)")
		tlsKey      = flag.String("tls-key", "", "TLS key file")
		workers     = flag.Int("workers", 8, "max concurrent RollCall operations")
		createAdmin = flag.String("create-admin", "", "create/reset an admin user as user:pass then exit")
		resetPass   = flag.String("reset-password", "", "reset a password as user:pass then exit")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("Demeter " + app.Version())
		return
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	if *listen != "" {
		cfg.ListenAddr = *listen
	}
	if *logLevel != "" {
		cfg.LoggingLevel = *logLevel
	}
	cfg.TLSCert, cfg.TLSKey = *tlsCert, *tlsKey

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	a, err := app.Build(ctx, cfg, *workers)
	if err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}

	// CLI account management (then exit).
	if *createAdmin != "" {
		runUserCmd(a.Auth, *createAdmin, auth.RoleAdmin, true)
		return
	}
	if *resetPass != "" {
		runUserCmd(a.Auth, *resetPass, auth.RoleOperator, false)
		return
	}

	if err := a.Bootstrap(); err != nil {
		slog.Error("admin bootstrap failed", "err", err)
	}

	if err := a.Run(ctx); err != nil && err.Error() != "http: Server closed" {
		slog.Error("server stopped", "err", err)
	}
	a.Shutdown()
}

func runUserCmd(a *auth.Auth, spec string, defRole auth.Role, create bool) {
	user, pass, ok := splitColon(spec)
	if !ok {
		slog.Error("expected user:pass")
		os.Exit(2)
	}
	if create {
		if err := a.CreateUser(user, pass, defRole); err != nil {
			// fall back to reset if it already exists
			if err := a.ResetPassword(user, pass); err != nil {
				slog.Error("create-admin failed", "err", err)
				os.Exit(1)
			}
			_ = a.SetRole(user, auth.RoleAdmin)
		}
		slog.Info("admin user ready", "username", user)
		return
	}
	if err := a.ResetPassword(user, pass); err != nil {
		slog.Error("reset-password failed", "err", err)
		os.Exit(1)
	}
	slog.Info("password reset", "username", user)
}

func splitColon(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "DemeterData"
	}
	return filepath.Join(home, "Documents", "DemeterData")
}

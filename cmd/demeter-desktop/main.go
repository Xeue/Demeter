//go:build desktop

// Command demeter-desktop is the "Electron-style" build: it runs the full
// Demeter web server on the configured address (default :8080, reachable by
// other operators' browsers just like the headless server) AND opens it in a
// native OS webview window (WebView2 on Windows, WebKit on macOS/Linux) — no
// bundled Chromium. The window auto-logs-in over loopback.
//
// Build with the `desktop` tag and CGO enabled:
//
//	CGO_ENABLED=1 go build -tags desktop -o demeter-desktop ./cmd/demeter-desktop
//
// The headless `cmd/demeter` server build is unaffected and stays pure-Go.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/Xeue/Demeter/internal/app"
	"github.com/Xeue/Demeter/internal/config"
	webview "github.com/webview/webview_go"
)

func main() {
	var (
		dataDir     = flag.String("data-dir", defaultDataDir(), "data directory")
		listen      = flag.String("listen", "", "web server listen address (overrides config, default :8080)")
		logLevel    = flag.String("log-level", "", "log level A|D|W|E (overrides config)")
		workers     = flag.Int("workers", 8, "max concurrent RollCall operations")
		user        = flag.String("user", "admin", "desktop session username (audited)")
		debug       = flag.Bool("devtools", false, "enable webview dev tools")
		mock        = flag.Bool("mock", false, "mock frames with cards for GUI dev (no hardware)")
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
	if *logLevel != "" {
		cfg.LoggingLevel = *logLevel
	}
	cfg.Mock = *mock
	if *listen != "" {
		cfg.ListenAddr = *listen
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := app.Build(ctx, cfg, *workers)
	if err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
	if err := a.Bootstrap(); err != nil {
		slog.Error("admin bootstrap failed", "err", err)
	}

	// Serve on the configured address (default :8080) so other browsers can reach
	// this instance — same as the headless server. Auth still applies.
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		slog.Error("could not start web server", "addr", cfg.ListenAddr, "err", err)
		os.Exit(1)
	}
	go func() {
		if err := a.RunListener(ctx, ln); err != nil && err.Error() != "http: Server closed" {
			slog.Error("server stopped", "err", err)
		}
	}()

	// Mint a session so the window auto-authenticates over loopback.
	token, err := a.DesktopSession(*user)
	if err != nil {
		slog.Error("could not create desktop session", "err", err)
		os.Exit(1)
	}
	// Point the window at loopback regardless of the bind address, so the
	// loopback-only /desktop-login auto-login is honoured.
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	slog.Info("Demeter desktop: web server listening", "addr", ln.Addr().String(), "browse", "http://<this-host>:"+port)
	url := "http://127.0.0.1:" + port + "/desktop-login?token=" + token

	w := webview.New(*debug)
	defer w.Destroy()
	w.SetTitle("Demeter")
	w.SetSize(1640, 1220, webview.HintNone)
	w.Navigate(url)
	w.Run() // blocks until the window is closed

	cancel()
	a.Shutdown()
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "DemeterData"
	}
	return filepath.Join(home, "Documents", "DemeterData")
}

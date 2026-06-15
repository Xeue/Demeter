//go:build desktop

// Command demeter-desktop is the "Electron-style" build: it runs the full
// Demeter server on a private loopback port and opens it in a native OS webview
// window (WebView2 on Windows, WebKit on macOS/Linux) — no bundled Chromium.
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
		logLevel    = flag.String("log-level", "", "log level A|D|W|E (overrides config)")
		workers     = flag.Int("workers", 8, "max concurrent RollCall operations")
		user        = flag.String("user", "admin", "desktop session username (audited)")
		debug       = flag.Bool("devtools", false, "enable webview dev tools")
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
	// Desktop always binds loopback only — the window is the only client.
	cfg.ListenAddr = "127.0.0.1:0"

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

	// Bind a loopback port first so we know the URL to point the window at.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		slog.Error("could not bind loopback port", "err", err)
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
	url := "http://" + ln.Addr().String() + "/desktop-login?token=" + token

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

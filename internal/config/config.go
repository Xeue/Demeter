// Package config loads Demeter's small configuration (the four legacy keys plus
// a listen address and data dir). It reads config.json, migrating the legacy
// xeue-config config.conf (which was already plain JSON) on first run.
package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Xeue/Demeter/internal/store"
)

// Config holds runtime configuration.
type Config struct {
	SystemName    string `json:"systemName"`
	LoggingLevel  string `json:"loggingLevel"` // A=all, D=debug, W=warn, E=error
	CreateLogFile bool   `json:"createLogFile"`
	DebugLineNum  bool   `json:"debugLineNum"`
	ListenAddr    string `json:"listenAddr"`

	// AutoReboot is the global default for auto-rebooting a card after a
	// restart-required change (a per-frame override can force it on/off). Off by
	// default. AutoRebootCooldownSeconds bounds how often one slot may be
	// auto-rebooted (a reboot takes ~1-2 min).
	AutoReboot                bool `json:"autoReboot"`
	AutoRebootCooldownSeconds int  `json:"autoRebootCooldownSeconds"`

	// ScanIntervalSeconds is the global scan/blast poll interval. Back it off on
	// large systems to reduce load. Clamped to [1, 3600] (see ScanInterval).
	ScanIntervalSeconds int `json:"scanIntervalSeconds"`

	// Not persisted (set from flags/env).
	DataDir string `json:"-"`
	TLSCert string `json:"-"`
	TLSKey  string `json:"-"`
}

// Defaults returns the built-in defaults (matching main.ts).
func Defaults() Config {
	return Config{
		SystemName:                "Demeter",
		LoggingLevel:              "W",
		CreateLogFile:             true,
		DebugLineNum:              false,
		ListenAddr:                ":8080",
		AutoReboot:                false,
		AutoRebootCooldownSeconds: 120,
		ScanIntervalSeconds:       3,
	}
}

// AutoRebootCooldown returns the cooldown as a duration (with a sane floor).
func (c Config) AutoRebootCooldown() time.Duration {
	s := c.AutoRebootCooldownSeconds
	if s <= 0 {
		s = 120
	}
	return time.Duration(s) * time.Second
}

// ScanIntervalBounds is the allowed range (seconds) for the global scan interval.
const (
	ScanIntervalMin = 1
	ScanIntervalMax = 3600
)

// ClampScanInterval clamps a seconds value into the allowed range, defaulting a
// zero/unset value to 3s.
func ClampScanInterval(seconds int) int {
	if seconds <= 0 {
		seconds = 3
	}
	if seconds < ScanIntervalMin {
		seconds = ScanIntervalMin
	}
	if seconds > ScanIntervalMax {
		seconds = ScanIntervalMax
	}
	return seconds
}

// ScanInterval returns the poll interval as a duration (clamped).
func (c Config) ScanInterval() time.Duration {
	return time.Duration(ClampScanInterval(c.ScanIntervalSeconds)) * time.Second
}

// Load reads dataDir/config.json, migrating dataDir/config.conf if present and
// config.json is not, else writing defaults.
func Load(dataDir string) (Config, error) {
	cfg := Defaults()
	cfg.DataDir = dataDir

	jsonPath := filepath.Join(dataDir, "config.json")
	confPath := filepath.Join(dataDir, "config.conf")

	switch {
	case fileExists(jsonPath):
		if err := store.ReadJSON(jsonPath, &cfg); err != nil {
			return cfg, err
		}
	case fileExists(confPath):
		// Legacy config.conf is plain JSON with the same keys.
		_ = store.ReadJSON(confPath, &cfg)
		if cfg.ListenAddr == "" {
			cfg.ListenAddr = ":8080"
		}
		_ = store.WriteJSON(jsonPath, cfg)
	default:
		_ = store.WriteJSON(jsonPath, cfg)
	}

	// Debug/all logging implies line numbers (main.ts:170).
	if cfg.LoggingLevel == "D" || cfg.LoggingLevel == "A" {
		cfg.DebugLineNum = true
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	return cfg, nil
}

// Save writes the config back to dataDir/config.json.
func (c Config) Save() error {
	return store.WriteJSON(filepath.Join(c.DataDir, "config.json"), c)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

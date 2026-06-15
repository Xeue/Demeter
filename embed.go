// Package demeter holds the embedded assets for the Demeter server: the command
// catalogue, the web GUI static files, and the page templates. These live at the
// repo root (above internal/) so a single set of //go:embed directives can reach
// them; internal packages import this package to get the embedded data.
package demeter

import "embed"

// CommandsJSON is the raw commandsDB.json, injected verbatim into the page and
// parsed by internal/commandsdb. The IDs are the same ones that appear on the
// RollCall wire, so they are used unchanged.
//
//go:embed commandsDB.json
var CommandsJSON []byte

// StaticFS is the static/ web asset tree (css, js, lib, fonts, img), served at
// /static/ by the web server.
//
//go:embed static
var StaticFS embed.FS

// ViewsFS holds the page templates (views/app.gohtml).
//
//go:embed views
var ViewsFS embed.FS

// VersionFile is the canonical app version (single source of truth, Go-native —
// not tied to the legacy npm package.json). The Makefile reads the same file.
//
//go:embed VERSION
var VersionFile string

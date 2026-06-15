# Demeter
A tool for bulk programing GV UCP cards.

## Usage
For Demeter to work, you must have rolltrak installed (install rollsuite) and rolltrak must be added to your windows system path.

`C:\Program Files (x86)\SAM\RollCallSuite\RollTrak.exe`
https://www.architectryan.com/2018/03/17/add-to-the-path-on-windows-10/

Create a "Group" and set desired properties for a group of UCP/IQ frames.
Add a frame via it's IP address and assign it to a group.

Then set frame to "scan" to discover cards and settings. Or, "scan & blast" to apply settings.

When the frame is set to "Scan & Blast" it will use rolltrak to connect to the frame and will apply settings based on the group.
Per card overrides can be applied once a card has been discovered.

The system will connect to a card via the frame to find it's IP addresses and will use the frame controler to change IP settings.
If it cannot reach the card via the cards IP it will not show any settings others than the IP settings and will not attempt to change them.
Once the card is reachable it will get and set settings directly to the card.

## Dev

### Go server (current)
Demeter now ships as a headless Go service that speaks the RollCall protocol
natively (no `rolltrak.exe`) and hosts the existing web GUI over HTTP +
WebSocket. The legacy Electron/TypeScript app is still in the tree for reference.

- Build: `go build ./cmd/demeter`
- Run: `./demeter` (defaults to `:8080`, data in `~/Documents/DemeterData`)
- Flags: `--listen`, `--data-dir`, `--log-level A|D|W|E`, `--workers`,
  `--tls-cert`/`--tls-key`, `--create-admin user:pass`, `--reset-password user:pass`
- First run prints a generated admin password (or set `DEMETER_ADMIN_USER` /
  `DEMETER_ADMIN_PASS`). Then browse to the listen address and log in.
- Existing `frames.json` / `groups.json` / `config.conf` are loaded/migrated
  automatically on first run.
- Tests: `go test -race ./...`

**Versioning:** the single source of truth is the `VERSION` file at the repo
root (Go-native — independent of the legacy npm `package.json`). The Makefile
stamps it into the binary (`-ldflags`) and into release artifact names (e.g.
`demeter-v2.0.0-windows-amd64.exe`); a plain `go build` falls back to the
embedded `VERSION` file, so the value is always correct. To release a new
version, edit `VERSION` only. The version is shown in the web GUI navbar and on
the login page (so users can quote it when reporting issues) and via
`demeter --version` / `make version`.

### Desktop app (native webview, Electron-style)
For a desktop-window experience without bundling Chromium, `cmd/demeter-desktop`
runs the same server on a private loopback port and opens it in the OS-native
webview (WebView2 on Windows, WebKit on macOS/Linux). It's the same Go binary
family — no Rust/Tauri toolchain — and auto-logs-in over loopback so there's no
login prompt.

- Build (needs CGO + the system webview headers): `make desktop` (or `CGO_ENABLED=1 go build -tags desktop -o demeter-desktop ./cmd/demeter-desktop`)
- Windows needs the WebView2 runtime (preinstalled on Win11; Evergreen installer on Win10).
- The plain `go build ./cmd/demeter` server build stays pure-Go static and is unaffected.
- If `go mod tidy` drops the webview dep, re-add it: `go get github.com/webview/webview_go`.

**Launching without a terminal window:**
- **Windows:** `make desktop-windows` builds with `-ldflags="-H windowsgui"`, so the
  `.exe` opens only the webview window (no console). Logs still go to the log file.
- **macOS:** `make desktop-macapp` produces `Demeter.app` — double-click it (or
  `open Demeter.app`) and Finder launches it with no Terminal. See
  `packaging/macos/Info.plist`.

See the [Makefile](Makefile) for all build targets (`server`, `desktop`,
`server-windows`, `desktop-windows`, `desktop-macapp`, `server-linux`).

Architecture: `cmd/demeter` wires `internal/{config,store,auth,logging,hub,
manager,frame,scan,device,pool,expr,model,commandsdb}`; `rollcall/` is the
native protocol client. The RollCall address packing, connect handshake and
offline-unit signal are isolated in `internal/device/{addr,session,offline}.go`
pending hardware confirmation (see `docs/ROLLCALL_PROTOCOL.md`).

### Legacy Electron app
Download source, "npm install" (or yarn or any other packagemanager of your choice)
To run in dev, "npm start"
To build, first comiple the typescript to js, "npm run compile"
Then build "npm run build"

## Thing's we've noticed

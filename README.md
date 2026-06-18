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
Demeter ships as a headless Go service that speaks the RollCall protocol
natively (no `rolltrak.exe`) and hosts the web GUI over HTTP + WebSocket. The
old Electron/TypeScript app has been removed from `main`; it is preserved on the
`legacy` branch (`git checkout legacy`) for reference only.

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
root (Go-native, independent of the legacy npm `package.json`). The Makefile
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
family (no Rust/Tauri toolchain) and auto-logs-in over loopback so there's no
login prompt.

- Build (needs CGO + the system webview headers): `make desktop`.
- Windows needs the WebView2 runtime (preinstalled on Win11; Evergreen installer on Win10).
- The plain `go build ./cmd/demeter` server build stays pure-Go static and is unaffected.
- If `go mod tidy` drops the webview dep, re-add it: `go get github.com/webview/webview_go`.

### Release artifact naming
Every target builds into `dist/v<version>/`, and all artifacts follow one
scheme: `Demeter-v<version>-<platform>[.ext]` (version from the `VERSION`
file). Desktop builds carry a `desktop-` token so they never collide with the
headless server on the same OS:

| Target | Output (under `dist/v<version>/`) |
|---|---|
| `make server-linux` | `Demeter-v<version>-linux-amd64` |
| `make server-linux-arm64` | `Demeter-v<version>-linux-arm64` |
| `make server-windows` | `Demeter-v<version>-windows-amd64.exe` |
| `make deb` (`DEB_ARCH=arm64` for ARM) | `Demeter-v<version>-linux-amd64.deb` |
| `make dist-linux` | `Demeter-v<version>-linux-amd64.tar.gz` |
| `make desktop` | `Demeter-v<version>-desktop-<os>-<arch>` (`.exe` on Windows) |
| `make desktop-windows` | `Demeter-v<version>-desktop-windows-amd64.exe` |
| `make desktop-macapp` | `Demeter-v<version>-desktop-macos.app` |

The plain `make server`/`make probe` also write into `dist/v<version>/` (as
`demeter`/`rcprobe`) for local dev/run. `dist/` is git-ignored; `make clean`
removes it.

**Launching the desktop app without a terminal window:**
- **Windows:** `make desktop-windows` builds with `-ldflags="-H windowsgui"`, so
  it opens only the webview window (no console). Logs still go to the log file.
- **macOS:** `make desktop-macapp` produces a `.app` bundle - double-click it
  (or `open Demeter-v<version>-desktop-macos.app`) and Finder launches it with no
  Terminal. See `packaging/macos/Info.plist`.

### Install on Linux as a service
For a non-technical operator with SSH access, build under `dist/v<version>/`, then either:
- **`.deb`:** `make deb`, copy the `.deb` over, then `sudo apt install ./Demeter-v<version>-linux-amd64.deb`.
- **Script:** `make dist-linux`, copy + untar the `.tar.gz`, then `sudo ./Demeter-v<version>-linux-amd64/install.sh`.

Both create a `demeter` system user + hardened systemd service, store data in
`/var/lib/demeter`, and print the URL + a generated admin login. See
[packaging/linux/install.sh](packaging/linux/install.sh) and [packaging/deb/](packaging/deb/).

**First login / admin password.** The generated admin password is saved to a
root-only file on the server (in addition to being printed by the installer):
```
sudo cat /var/lib/demeter/INITIAL_ADMIN_PASSWORD
```
The server writes this on first run, so it's there regardless of install method.
If the file is ever missing, the password is also in the service log:
`sudo journalctl -u demeter | grep -i "ADMIN BOOTSTRAP"`. Log in (default user
`admin`), change the password in the GUI, then `sudo rm` that file. To set/reset
it from the shell at any time:
```
# script install:
sudo /path/to/install.sh set-password           # prompts (or auto-generates)
# either install (stop, set, start):
sudo systemctl stop demeter
sudo -u demeter demeter --create-admin admin:NEW_PASSWORD --data-dir /var/lib/demeter
sudo systemctl start demeter
```
(The in-GUI "generated credentials" notice only appears to a loopback/desktop
admin, never to a remote browser — so use the file or the journal above.)

Run `make` (or `make list`) for the full target list.

Architecture: `cmd/demeter` wires `internal/{config,store,auth,logging,hub,
manager,frame,scan,device,pool,expr,model,commandsdb}`; `rollcall/` is the
native protocol client. The RollCall address packing, connect handshake and
offline-unit signal are isolated in `internal/device/{addr,session,offline}.go`
pending hardware confirmation (see `docs/ROLLCALL_PROTOCOL.md`).

### Legacy Electron app (removed)
The original Electron/TypeScript app (and its `npm`/`electron-builder` toolchain)
has been removed from `main`. It lives on the `legacy` branch if you ever need it:
`git checkout legacy`.

## Thing's we've noticed

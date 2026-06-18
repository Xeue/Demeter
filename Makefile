# Demeter build targets.
#
# Two binaries:
#   demeter          - headless server, PURE Go (CGO off), trivially cross-compiles
#   demeter-desktop  - native-webview desktop app (CGO + system webview), tag `desktop`
#
# Version is the single source of truth from the VERSION file; it is stamped into
# the binary (ldflags) and into release artifact names. Bump it in VERSION.
#
# Windows desktop builds need a mingw-w64 C++ cross-compiler (macOS: `brew install
# mingw-w64`) OR build natively on Windows with MSYS2/mingw-w64. The WebView2
# runtime must be present on the Windows machine (preinstalled on Win11).

BIN      ?= demeter
DESKTOP  ?= demeter-desktop
APP_NAME ?= Demeter
MINGW_CC  ?= x86_64-w64-mingw32-gcc
MINGW_CXX ?= x86_64-w64-mingw32-g++

# Single source of truth: the VERSION file.
VERSION := $(shell tr -d '[:space:]' < VERSION)
LDFLAGS := -X github.com/Xeue/Demeter/internal/app.versionOverride=$(VERSION)

# All release artifacts follow one scheme: Demeter-v<version>-<platform>[.ext]
#   headless server : Demeter-v<version>-<os>-<arch>          (.exe on Windows)
#   debian package  : Demeter-v<version>-linux-<arch>.deb
#   linux tarball   : Demeter-v<version>-linux-<arch>.tar.gz
#   desktop app     : Demeter-v<version>-desktop-<os>-<arch>  (.exe Win / .app macOS)
# (the plain `server`/`probe` dev builds keep their short names for local use.)
ART         := $(APP_NAME)-v$(VERSION)
HOST_GOOS   := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)
HOST_EXT    := $(if $(filter windows,$(HOST_GOOS)),.exe,)

# Everything builds into a per-version output folder: dist/v<version>/
DIST := dist/v$(VERSION)
OUT  := $(DIST)/$(ART)

.PHONY: version server desktop desktop-macapp server-windows desktop-windows server-linux server-linux-arm64 deb deb-arm64 dist-linux probe probe-windows test clean

# Debian package: arch + output name (override DEB_ARCH=arm64 for ARM boxes).
DEB_ARCH ?= amd64
DEB_OUT   = $(OUT)-linux-$(DEB_ARCH).deb

.PHONY: help list
.DEFAULT_GOAL := help

## List the available targets (this list; also: make list)
help:
	@echo "Demeter make targets (version $(VERSION)):"
	@echo ""
	@awk 'BEGIN{FS=":"} \
		/^## / { d=substr($$0,4); if (desc=="") desc=d; next } \
		/^[a-zA-Z0-9_-]+:/ { if (desc!="") { printf "  \033[36m%-20s\033[0m %s\n", $$1, desc; desc="" } } \
		/^$$/ { desc="" }' $(MAKEFILE_LIST)
	@echo ""

list: help

## Print the version Make will stamp/name with
version:
	@echo $(VERSION)

## Native headless server (current OS/arch), pure-Go static binary
server:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(DIST)/$(BIN) ./cmd/demeter
	@echo "Built $(DIST)/$(BIN) — run: ./$(DIST)/$(BIN)"

## Native desktop app (current OS) -> Demeter-v<version>-desktop-<os>-<arch>[.exe]
desktop:
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -tags desktop -ldflags="$(LDFLAGS)" -o $(OUT)-desktop-$(HOST_GOOS)-$(HOST_GOARCH)$(HOST_EXT) ./cmd/demeter-desktop
	@echo "Built $(OUT)-desktop-$(HOST_GOOS)-$(HOST_GOARCH)$(HOST_EXT)"

## macOS .app bundle -> Demeter-v<version>-desktop-macos.app (double-clicks from Finder, NO terminal)
desktop-macapp:
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -tags desktop -ldflags="$(LDFLAGS)" -o /tmp/$(DESKTOP)-macapp ./cmd/demeter-desktop
	rm -rf $(OUT)-desktop-macos.app
	mkdir -p $(OUT)-desktop-macos.app/Contents/MacOS $(OUT)-desktop-macos.app/Contents/Resources
	sed 's/__VERSION__/$(VERSION)/' packaging/macos/Info.plist > $(OUT)-desktop-macos.app/Contents/Info.plist
	cp /tmp/$(DESKTOP)-macapp $(OUT)-desktop-macos.app/Contents/MacOS/$(DESKTOP)
	-sips -s format icns static/img/icon/icon.png --out $(OUT)-desktop-macos.app/Contents/Resources/icon.icns >/dev/null 2>&1 || true
	@echo "Built $(OUT)-desktop-macos.app (v$(VERSION)) — double-click it, or run: open $(OUT)-desktop-macos.app"

## Headless server for Windows x64 (cross-compiles from anywhere, no C toolchain)
server-windows:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(OUT)-windows-amd64.exe ./cmd/demeter
	@echo "Built $(OUT)-windows-amd64.exe"

## Desktop app for Windows x64 -> Demeter-v<version>-desktop-windows-amd64.exe (needs mingw-w64 C++; -H windowsgui hides the console)
desktop-windows:
	@mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=$(MINGW_CC) CXX=$(MINGW_CXX) \
		go build -tags desktop -ldflags="$(LDFLAGS) -H windowsgui" -o $(OUT)-desktop-windows-amd64.exe ./cmd/demeter-desktop
	@echo "Built $(OUT)-desktop-windows-amd64.exe"

## Headless server for Linux x64 -> Demeter-v<version>-linux-amd64
server-linux:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(OUT)-linux-amd64 ./cmd/demeter
	@echo "Built $(OUT)-linux-amd64"

## Headless server for Linux ARM64 (Raspberry Pi / ARM servers)
server-linux-arm64:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(OUT)-linux-arm64 ./cmd/demeter
	@echo "Built $(OUT)-linux-arm64"

## Self-contained Linux tarball: binary + install.sh, ready to scp to a server.
## Untar on the box, then: sudo ./Demeter-v<version>-linux-amd64/install.sh
dist-linux: server-linux
	@mkdir -p $(DIST)
	rm -rf /tmp/demeter-dist
	install -d /tmp/demeter-dist/$(ART)-linux-amd64
	install -m 0755 $(OUT)-linux-amd64 /tmp/demeter-dist/$(ART)-linux-amd64/demeter
	install -m 0755 packaging/linux/install.sh /tmp/demeter-dist/$(ART)-linux-amd64/install.sh
	tar -C /tmp/demeter-dist -czf $(OUT)-linux-amd64.tar.gz $(ART)-linux-amd64
	@echo "Built $(OUT)-linux-amd64.tar.gz  ->  scp to server, untar, 'sudo ./$(ART)-linux-amd64/install.sh'"

## Debian package (.deb) installing demeter as a systemd service.
## Needs dpkg-deb (macOS: 'brew install dpkg'; Debian/CI: built in).
## ARM: make deb DEB_ARCH=arm64
deb:
	@command -v dpkg-deb >/dev/null 2>&1 || { echo "dpkg-deb not found — macOS: 'brew install dpkg'; or build on a Debian box / CI"; exit 1; }
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(DEB_ARCH) go build -ldflags="$(LDFLAGS)" -o /tmp/demeter-deb-bin ./cmd/demeter
	rm -rf /tmp/demeter-deb
	install -d /tmp/demeter-deb/DEBIAN /tmp/demeter-deb/usr/bin /tmp/demeter-deb/lib/systemd/system
	install -m 0755 /tmp/demeter-deb-bin /tmp/demeter-deb/usr/bin/demeter
	sed -e 's,__BINPATH__,/usr/bin/demeter,' -e 's,__LISTEN__,:8080,' \
		packaging/linux/demeter.service > /tmp/demeter-deb/lib/systemd/system/demeter.service
	sed -e 's,__VERSION__,$(VERSION),' -e 's,__ARCH__,$(DEB_ARCH),' \
		packaging/deb/control > /tmp/demeter-deb/DEBIAN/control
	install -m 0755 packaging/deb/postinst packaging/deb/prerm packaging/deb/postrm /tmp/demeter-deb/DEBIAN/
	dpkg-deb --build --root-owner-group /tmp/demeter-deb $(DEB_OUT)
	@echo "Built $(DEB_OUT)  ->  copy to server, then: sudo apt install ./$(DEB_OUT)"

## Convenience: arm64 .deb
deb-arm64:
	$(MAKE) deb DEB_ARCH=arm64

## RollCall connectivity probe (current OS) — diagnose "Cannot reach frame"
probe:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 go build -o $(DIST)/rcprobe ./cmd/rcprobe
	@echo "Built $(DIST)/rcprobe — run: ./$(DIST)/rcprobe -frame <frameIP>"

## RollCall probe for Windows x64 (ship rcprobe.exe to a site to test their frame)
probe-windows:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(DIST)/rcprobe-windows-amd64.exe ./cmd/rcprobe
	@echo "Built $(DIST)/rcprobe-windows-amd64.exe — run: rcprobe-windows-amd64.exe -frame <frameIP>"

## Run the full test suite with the race detector
test:
	go test -race ./...

## Remove the dist/ output folder (and any legacy root-level artifacts)
clean:
	rm -rf dist
	rm -f $(BIN) $(DESKTOP) rcprobe rcprobe-* $(BIN)-v*-* $(BIN)_*.deb $(BIN)-v*.tar.gz
	rm -rf $(APP_NAME)-v* $(APP_NAME).app   # legacy root-level names

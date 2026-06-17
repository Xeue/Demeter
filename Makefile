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

# Desktop release artifacts are named Demeter-v<version> with a platform-
# appropriate extension: .exe on Windows, .app bundle on macOS, none on Linux.
DESKTOP_OUT := $(APP_NAME)-v$(VERSION)
HOST_GOOS   := $(shell go env GOOS)
HOST_EXT    := $(if $(filter windows,$(HOST_GOOS)),.exe,)

.PHONY: version server desktop desktop-macapp server-windows desktop-windows server-linux probe probe-windows test clean

## Print the version Make will stamp/name with
version:
	@echo $(VERSION)

## Native headless server (current OS/arch), pure-Go static binary
server:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/demeter

## Native desktop app (current OS), CGO + OS webview -> Demeter-v<version>[.exe]
desktop:
	CGO_ENABLED=1 go build -tags desktop -ldflags="$(LDFLAGS)" -o $(DESKTOP_OUT)$(HOST_EXT) ./cmd/demeter-desktop
	@echo "Built $(DESKTOP_OUT)$(HOST_EXT)"

## macOS .app bundle -> Demeter-v<version>.app (double-clicks from Finder, NO terminal)
desktop-macapp:
	CGO_ENABLED=1 go build -tags desktop -ldflags="$(LDFLAGS)" -o /tmp/$(DESKTOP)-macapp ./cmd/demeter-desktop
	rm -rf $(DESKTOP_OUT).app
	mkdir -p $(DESKTOP_OUT).app/Contents/MacOS $(DESKTOP_OUT).app/Contents/Resources
	sed 's/__VERSION__/$(VERSION)/' packaging/macos/Info.plist > $(DESKTOP_OUT).app/Contents/Info.plist
	cp /tmp/$(DESKTOP)-macapp $(DESKTOP_OUT).app/Contents/MacOS/$(DESKTOP)
	-sips -s format icns static/img/icon/icon.png --out $(DESKTOP_OUT).app/Contents/Resources/icon.icns >/dev/null 2>&1 || true
	@echo "Built $(DESKTOP_OUT).app (v$(VERSION)) — double-click it, or run: open $(DESKTOP_OUT).app"

## Headless server for Windows x64 (cross-compiles from anywhere, no C toolchain)
server-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BIN)-v$(VERSION)-windows-amd64.exe ./cmd/demeter

## Desktop app for Windows x64 -> Demeter-v<version>.exe (needs mingw-w64 C++; -H windowsgui hides the console)
desktop-windows:
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=$(MINGW_CC) CXX=$(MINGW_CXX) \
		go build -tags desktop -ldflags="$(LDFLAGS) -H windowsgui" -o $(DESKTOP_OUT).exe ./cmd/demeter-desktop
	@echo "Built $(DESKTOP_OUT).exe"

## Headless server for Linux x64
server-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BIN)-v$(VERSION)-linux-amd64 ./cmd/demeter

## RollCall connectivity probe (current OS) — diagnose "Cannot reach frame"
probe:
	CGO_ENABLED=0 go build -o rcprobe ./cmd/rcprobe
	@echo "Built rcprobe — run: ./rcprobe -frame <frameIP>"

## RollCall probe for Windows x64 (ship rcprobe.exe to a site to test their frame)
probe-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o rcprobe-windows-amd64.exe ./cmd/rcprobe
	@echo "Built rcprobe-windows-amd64.exe — run: rcprobe-windows-amd64.exe -frame <frameIP>"

test:
	go test -race ./...

clean:
	rm -f $(BIN) $(BIN)-v*-* $(DESKTOP) $(DESKTOP)-v* rcprobe rcprobe-*
	rm -rf $(APP_NAME).app $(APP_NAME)-v*

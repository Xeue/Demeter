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

.PHONY: version server desktop desktop-macapp server-windows desktop-windows server-linux test clean

## Print the version Make will stamp/name with
version:
	@echo $(VERSION)

## Native headless server (current OS/arch), pure-Go static binary
server:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/demeter

## Native desktop app (current OS), CGO + OS webview
desktop:
	CGO_ENABLED=1 go build -tags desktop -ldflags="$(LDFLAGS)" -o $(DESKTOP) ./cmd/demeter-desktop

## macOS .app bundle — double-clicks from Finder with NO terminal window
desktop-macapp:
	CGO_ENABLED=1 go build -tags desktop -ldflags="$(LDFLAGS)" -o /tmp/$(DESKTOP)-macapp ./cmd/demeter-desktop
	rm -rf $(APP_NAME).app
	mkdir -p $(APP_NAME).app/Contents/MacOS $(APP_NAME).app/Contents/Resources
	sed 's/__VERSION__/$(VERSION)/' packaging/macos/Info.plist > $(APP_NAME).app/Contents/Info.plist
	cp /tmp/$(DESKTOP)-macapp $(APP_NAME).app/Contents/MacOS/$(DESKTOP)
	-sips -s format icns static/img/icon/icon.png --out $(APP_NAME).app/Contents/Resources/icon.icns >/dev/null 2>&1 || true
	@echo "Built $(APP_NAME).app (v$(VERSION)) — double-click it, or run: open $(APP_NAME).app"

## Headless server for Windows x64 (cross-compiles from anywhere, no C toolchain)
server-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BIN)-v$(VERSION)-windows-amd64.exe ./cmd/demeter

## Desktop app for Windows x64 (needs mingw-w64 C++; -H windowsgui hides the console)
desktop-windows:
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=$(MINGW_CC) CXX=$(MINGW_CXX) \
		go build -tags desktop -ldflags="$(LDFLAGS) -H windowsgui" -o $(DESKTOP)-v$(VERSION)-windows-amd64.exe ./cmd/demeter-desktop

## Headless server for Linux x64
server-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BIN)-v$(VERSION)-linux-amd64 ./cmd/demeter

test:
	go test -race ./...

clean:
	rm -f $(BIN) $(BIN)-v*-* $(DESKTOP) $(DESKTOP)-v*-*
	rm -rf $(APP_NAME).app

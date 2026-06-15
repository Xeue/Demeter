//go:build !desktop

// This stub is built when the `desktop` tag is absent so that plain
// `go build ./...` / `go test ./...` succeed without the CGO/webview toolchain.
// The real desktop launcher lives in main.go behind `//go:build desktop`.
package main

import "fmt"

func main() {
	fmt.Println("demeter-desktop must be built with the desktop tag and CGO enabled:")
	fmt.Println("  CGO_ENABLED=1 go build -tags desktop ./cmd/demeter-desktop")
}

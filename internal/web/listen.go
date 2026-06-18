package web

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
)

// MaxPortProbes is how many consecutive ports Listen tries before giving up
// (e.g. 8080..8179) — bounded so it can't climb indefinitely.
const MaxPortProbes = 100

// Listen binds a TCP listener at addr. If the requested port is already in use,
// it tries the next ports (port+1, +2, …) up to MaxPortProbes and returns the
// first that binds — so Demeter still starts when another app holds the default
// port (e.g. :8080). The returned listener's Addr() reports the port actually
// bound; callers should use that. A non-"address in use" error (bad host,
// permission, etc.) is returned immediately, since another port won't help.
func Listen(addr string) (net.Listener, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	base, err := strconv.Atoi(portStr)
	if err != nil || base == 0 {
		// Non-numeric port or :0 (OS-assigned) — nothing to probe; bind as given.
		return net.Listen("tcp", addr)
	}
	var lastErr error
	for i := 0; i < MaxPortProbes; i++ {
		a := net.JoinHostPort(host, strconv.Itoa(base+i))
		ln, err := net.Listen("tcp", a)
		if err == nil {
			return ln, nil
		}
		lastErr = err
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no free port in %d..%d: %w", base, base+MaxPortProbes-1, lastErr)
}

// PortOf returns the port component of an address (e.g. ":8080" or
// "127.0.0.1:8080" → "8080"), or "" if it can't be parsed.
func PortOf(addr string) string {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return p
}

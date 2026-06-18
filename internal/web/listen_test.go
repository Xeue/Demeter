package web

import (
	"net"
	"strconv"
	"testing"
)

// TestListenProbesNextPort: when the requested port is in use, Listen binds the
// next free one instead of failing.
func TestListenProbesNextPort(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	addr := occupied.Addr().String() // 127.0.0.1:<port>
	base := PortOf(addr)

	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen should have probed to a free port, got: %v", err)
	}
	defer ln.Close()

	got := PortOf(ln.Addr().String())
	if got == base {
		t.Fatalf("expected a different port than the occupied %s, got %s", base, got)
	}
	bi, _ := strconv.Atoi(base)
	gi, _ := strconv.Atoi(got)
	if gi < bi || gi >= bi+MaxPortProbes {
		t.Errorf("bound port %d outside the probe window [%d,%d)", gi, bi, bi+MaxPortProbes)
	}
}

// TestListenFreePortUnchanged: a free requested port is bound as-is.
func TestListenFreePortUnchanged(t *testing.T) {
	// Grab a free port, release it, then ask Listen for it.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	want := PortOf(addr)
	probe.Close()

	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	if got := PortOf(ln.Addr().String()); got != want {
		t.Errorf("free port changed: want %s got %s", want, got)
	}
}

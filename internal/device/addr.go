package device

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Xeue/Demeter/rollcall"
)

// SEAM #1 — CLI addressing -> rollcall.Addr.
//
// Demeter addresses a target as `cmd@<net>:<addr>:<slot>` where <addr> is a hex
// token (the frame's discovered unit address such as "12"/"30") and <slot> is a
// hex token. The mapping is now CONFIRMED against captures (see
// docs/ROLLCALL_PROTOCOL.md): unit = (addr<<8)|slot, net = 0, port = 0. E.g.
// @0000:12:05 -> 0x1205, direct-to-card @0000:30:00 -> 0x3000.
//
// AddrMapper stays a package-level variable so the mapping can be overridden
// (e.g. for a frame that needs a non-zero port in connected mode) without
// touching the scan layer.
var AddrMapper = defaultAddrMapper

// defaultAddrMapper packs the CLI form into the wire address using the confirmed
// mapping; see rollcall.UnitAddr.
func defaultAddrMapper(addr, slot string) (rollcall.Addr, error) {
	a, err := strconv.ParseUint(strings.TrimSpace(addr), 16, 8)
	if err != nil {
		return rollcall.Addr{}, fmt.Errorf("device: bad addr token %q: %w", addr, err)
	}
	s, err := strconv.ParseUint(strings.TrimSpace(slot), 16, 8)
	if err != nil {
		return rollcall.Addr{}, fmt.Errorf("device: bad slot token %q: %w", slot, err)
	}
	return rollcall.UnitAddr(uint8(a), uint8(s)), nil
}

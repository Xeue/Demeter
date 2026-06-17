package device

import "github.com/Xeue/Demeter/rollcall"

// SEAM #2 — connect handshake.
//
// rolltrak (the legacy client, which worked) spoke RollCall "unconnected" mode;
// the Go client speaks "connected" mode (persistent + notifies). A connected
// session in the capture begins with the client announcing itself (IDENTITY_SELF,
// opcode 0x21) before GETs are answered. The Go client.Dial connects raw, so a
// frame that requires that login answers nothing — which surfaces as
// "Cannot reach frame".
//
// startSession (gated by Dialer.Handshake, default off) replays the captured
// IDENTITY_SELF so such a frame will accept us. It is best-effort from the
// Rollcall.pcapng capture and should be validated on hardware; the deeper
// connected-mode addressing question (session-handle ports vs the port-0 we
// send) is tracked in docs/ROLLCALL_PROTOCOL.md.
func startSession(c *rollcall.Client, enabled bool) {
	if !enabled {
		return
	}
	// IDENTITY_SELF is a broadcast to net0:unit0:port0x00ff with opcode 0x21 and
	// flags 0x40, followed by the captured login body (client name embedded).
	bcast := rollcall.Addr{Net: 0, Unit: 0, Port: 0x00ff}
	_ = c.Send(rollcall.Message{
		Dst:    bcast,
		Src:    bcast,
		Opcode: rollcall.OpIdentSelf,
		Flags:  0x40,
		Raw:    identitySelfBody,
	})
}

// identitySelfBody is the IDENTITY_SELF payload (everything after opcode 0x21 +
// flags 0x40), copied verbatim from a real Control Panel login in
// Rollcall.pcapng (the client announced the name "ControlPanel"). Replayed as-is
// because the header/version fields are not yet decoded.
var identitySelfBody = []byte{
	0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xf4,
	0x04, 0x15, 0x20, 0x05,
	'C', 'o', 'n', 't', 'r', 'o', 'l', 'P', 'a', 'n', 'e', 'l',
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08,
}

package device

import "github.com/Xeue/Demeter/rollcall"

// SEAM #2 — connect handshake + keepalive.
//
// The rollcall.Client does not send IDENTITY/OPEN on Dial, and the capture shows
// periodic OPEN/ACK that may be required to keep units answering. Whether a unit
// must be OPENed before GET/SET works, and whether a keepalive is needed, is not
// yet confirmed on hardware. startSession is therefore gated by Dialer.Keepalive
// (default off) and is a stub that the rollcall owner fills in via Client.Send
// once the handshake is known. Keeping it here means the scan layer never sees
// session management.
func startSession(c *rollcall.Client, enabled bool) {
	if !enabled {
		return
	}
	// TODO(hardware): send IDENTITY (OpIdentSelf) on connect and run a keepalive
	// goroutine issuing OPEN (OpOpen) per touched unit, using c.Send(...). Left
	// as a no-op until the handshake is confirmed against a real frame.
	_ = c
}

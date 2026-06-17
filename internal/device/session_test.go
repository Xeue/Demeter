package device

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/Xeue/Demeter/rollcall"
)

// TestIdentitySelfMatchesCapture proves the connect-handshake message we replay
// is byte-identical to the IDENTITY_SELF a real Control Panel sent on connect in
// Rollcall.pcapng (magic..end). If this passes, the login bytes are correct; any
// remaining failure is addressing/mode, not the handshake encoding.
func TestIdentitySelfMatchesCapture(t *testing.T) {
	want, err := hex.DecodeString(
		"000c0038" + // magic 00 0c + outerLen 0x0038 (56)
			"0000000000ff" + // dst  net0:unit0:port0x00ff (broadcast)
			"0000000000ff" + // src  same
			"002a" + // innerLen 0x002a (42)
			"2140" + // opcode 0x21 (IDENTITY_SELF) + flags 0x40
			"0003000000000000000001f404152005" + // header/version fields (undecoded)
			"436f6e74726f6c50616e656c" + // "ControlPanel"
			"000000000000000000000008") // name padding + trailing 0x08
	if err != nil {
		t.Fatal(err)
	}

	got := rollcall.Message{
		Dst:    rollcall.Addr{Net: 0, Unit: 0, Port: 0x00ff},
		Src:    rollcall.Addr{Net: 0, Unit: 0, Port: 0x00ff},
		Opcode: rollcall.OpIdentSelf,
		Flags:  0x40,
		Raw:    identitySelfBody,
	}.Encode()

	if !bytes.Equal(got, want) {
		t.Errorf("IDENTITY_SELF wire bytes differ from the capture:\n got  %x\n want %x", got, want)
	}
}

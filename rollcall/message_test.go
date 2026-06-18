package rollcall

import (
	"encoding/hex"
	"reflect"
	"testing"
)

// Real bytes captured from a RollCall Control Panel session (Rollcall.pcapng).
// client 10.40.40.190 <-> frame 10.40.44.10:2050. These are the ground truth the
// codec is validated against - no hardware required.
//
// unit = 0000:1005:0001, client = 0000:0000:0005
var (
	unit   = Addr{Net: 0x0000, Unit: 0x1005, Port: 0x0001}
	client = Addr{Net: 0x0000, Unit: 0x0000, Port: 0x0005}
)

type fixture struct {
	name string
	hex  string
	msg  Message
}

var fixtures = []fixture{
	{
		name: "GET 3500",
		hex:  "000c00140000100500010000000000050006450000000dac",
		msg:  Message{Dst: unit, Src: client, Opcode: OpGet, CmdID: 3500},
	},
	{
		name: "GET 4108",
		hex:  "000c0014000010050001000000000005000645000000100c",
		msg:  Message{Dst: unit, Src: client, Opcode: OpGet, CmdID: 4108},
	},
	{
		name: "SET 48729 int 0",
		hex:  "000c001c000010050001000000000005000e46000000be590000000100000000",
		msg:  Message{Dst: unit, Src: client, Opcode: OpSet, CmdID: 48729, Value: Int(0)},
	},
	{
		name: "REPLY 3500 int 0",
		hex:  "000c001c000000000005000010050001000e470000000dac0000000100000000",
		msg:  Message{Dst: client, Src: unit, Opcode: OpReply, CmdID: 3500, Value: Int(0)},
	},
	{
		name: "REPLY 4108 int 1",
		hex:  "000c001c000000000005000010050001000e47000000100c0000000100000001",
		msg:  Message{Dst: client, Src: unit, Opcode: OpReply, CmdID: 4108, Value: Int(1)},
	},
	{
		name: "REPLY 4101 string IP",
		hex:  "000c0029000000000005000010050001001b470000001005000000020000000031302e3130302e34342e313200",
		msg:  Message{Dst: client, Src: unit, Opcode: OpReply, CmdID: 4101, Value: Str("10.100.44.12")},
	},
	{
		name: "REPLY 37139 string notify (PTP offset)",
		hex:  "000c0026000000000005000010050001001847800000911300000002000000002020202d302e30755300",
		msg:  Message{Dst: client, Src: unit, Opcode: OpReply, Flags: FlagNotify, CmdID: 37139, Value: Str("   -0.0uS")},
	},
}

func TestEncodeMatchesCapture(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			got := hex.EncodeToString(f.msg.Encode())
			if got != f.hex {
				t.Errorf("Encode mismatch\n want %s\n  got %s", f.hex, got)
			}
		})
	}
}

func TestDecodeMatchesCapture(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			raw, err := hex.DecodeString(f.hex)
			if err != nil {
				t.Fatal(err)
			}
			m, n, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if n != len(raw) {
				t.Errorf("consumed %d want %d", n, len(raw))
			}
			if m.Dst != f.msg.Dst || m.Src != f.msg.Src {
				t.Errorf("addr: got dst=%s src=%s want dst=%s src=%s", m.Dst, m.Src, f.msg.Dst, f.msg.Src)
			}
			if m.Opcode != f.msg.Opcode || m.Flags != f.msg.Flags || m.CmdID != f.msg.CmdID {
				t.Errorf("op/flags/cmd: got %s f=%02x cmd=%d want %s f=%02x cmd=%d",
					m.Opcode, m.Flags, m.CmdID, f.msg.Opcode, f.msg.Flags, f.msg.CmdID)
			}
			if !reflect.DeepEqual(m.Value, f.msg.Value) {
				t.Errorf("value: got %s want %s", m.Value, f.msg.Value)
			}
		})
	}
}

// Decode(Encode(x)) == x for every fixture, and re-encoding a decoded capture
// reproduces the original bytes exactly.
func TestRoundTrip(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			raw, _ := hex.DecodeString(f.hex)
			m, _, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			if again := hex.EncodeToString(m.Encode()); again != f.hex {
				t.Errorf("re-encode mismatch\n want %s\n  got %s", f.hex, again)
			}
		})
	}
}

// An unfamiliar dataType (here 3) must be surfaced as KindUnknown with its raw
// bytes preserved, not silently dropped to KindNone.
func TestDecodeUnknownKind(t *testing.T) {
	// REPLY cmd=4108 with dataType=3 and a 4-byte payload deadbeef.
	m := Message{Dst: client, Src: unit, Opcode: OpReply, CmdID: 4108}
	inner := []byte{byte(OpReply), 0x00, 0x00, 0x00, 0x10, 0x0c, 0x00, 0x00, 0x00, 0x03, 0xde, 0xad, 0xbe, 0xef}
	body := make([]byte, 14+len(inner))
	putAddr(body[0:6], m.Dst)
	putAddr(body[6:12], m.Src)
	body[13] = byte(len(inner))
	copy(body[14:], inner)
	raw := append([]byte{0x00, 0x0c, 0x00, byte(len(body))}, body...)

	got, _, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value.Kind != KindUnknown || got.Value.RawType != 3 {
		t.Fatalf("got kind=%d type=%d, want KindUnknown type=3", got.Value.Kind, got.Value.RawType)
	}
	if want := []byte{0xde, 0xad, 0xbe, 0xef}; !reflect.DeepEqual(got.Value.Raw, want) {
		t.Errorf("raw = % x, want % x", got.Value.Raw, want)
	}
}

// Confirmed against the rolltrak capture: cmd@<net>:<addr>:<slot> packs as
// unit = (addr<<8)|slot, net=0, port=0.
func TestUnitAddr(t *testing.T) {
	cases := []struct {
		addr, slot uint8
		want       uint16
	}{
		{0x12, 0x00, 0x1200}, // frame controller, slot 0
		{0x12, 0x05, 0x1205}, // card in slot 5 via frame
		{0x30, 0x00, 0x3000}, // card addressed directly on its own IP
		{0x10, 0x05, 0x1005}, // matches the first capture's unit
	}
	for _, c := range cases {
		a := UnitAddr(c.addr, c.slot)
		if a.Unit != c.want || a.Net != 0 || a.Port != 0 {
			t.Errorf("UnitAddr(%#x,%#x) = %s, want unit %04x net 0 port 0", c.addr, c.slot, a, c.want)
		}
	}
}

func TestDecodeShortBuffer(t *testing.T) {
	raw, _ := hex.DecodeString(fixtures[0].hex)
	if _, _, err := Decode(raw[:len(raw)-1]); err != ErrShort {
		t.Errorf("want ErrShort, got %v", err)
	}
	if _, _, err := Decode([]byte{0x01, 0x02, 0x03, 0x04}); err != ErrMagic {
		t.Errorf("want ErrMagic, got %v", err)
	}
}

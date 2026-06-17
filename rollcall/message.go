package rollcall

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Magic is the 2-byte prefix every RollCall message starts with.
var Magic = [2]byte{0x00, 0x0c}

// Opcode is the first byte of a message's inner payload.
type Opcode byte

// Connected-session opcodes — what the RollCall Control Panel uses and what this
// Client implements. Command id and dataType are uint32.
const (
	OpAck       Opcode = 0x01 // server -> client: acknowledges an OPEN
	OpIdentUnit Opcode = 0x14 // server -> client: a unit announces its name
	OpOpen      Opcode = 0x1c // client -> server: attach to / subscribe a unit
	OpIdentSelf Opcode = 0x21 // client -> server: announce our own name
	OpGet       Opcode = 0x45 // client -> server: read a parameter
	OpSet       Opcode = 0x46 // client -> server: write a parameter
	OpReply     Opcode = 0x47 // server -> client: current value (solicited reply or notify)
)

// Unconnected ("RollTrak-style") opcodes. RollTrak.exe doesn't hold a session: it
// broadcasts a login (OpLogin), the target announces itself (OpIdentUnit), then it
// sends OpUReq and gets OpUReply or OpNack. In this mode the command id and
// dataType are uint16 (not uint32) and the address port is 0. Documented for
// completeness / a possible alternative transport — see docs/ROLLCALL_PROTOCOL.md.
const (
	OpNack   Opcode = 0x00 // negative ack / error (carries no command id)
	OpUReq   Opcode = 0x0b // unconnected request
	OpUReply Opcode = 0x0c // unconnected reply
	OpLogin  Opcode = 0x15 // unconnected broadcast login
)

// FlagNotify in a reply's flags byte marks an unsolicited update (value changed
// on the device) rather than a direct answer to a GET/SET.
const FlagNotify byte = 0x80

func (o Opcode) String() string {
	switch o {
	case OpAck:
		return "ACK"
	case OpIdentUnit:
		return "IDENT_UNIT"
	case OpOpen:
		return "OPEN"
	case OpIdentSelf:
		return "IDENT_SELF"
	case OpGet:
		return "GET"
	case OpSet:
		return "SET"
	case OpReply:
		return "REPLY"
	default:
		return fmt.Sprintf("0x%02x", byte(o))
	}
}

// connectedData reports whether the opcode is a connected-mode data op (uint32
// command id + uint32 dataType): GET/SET/REPLY.
func (o Opcode) connectedData() bool { return o == OpGet || o == OpSet || o == OpReply }

// unconnectedData reports whether the opcode is an unconnected-mode data op
// (uint16 command id + uint16 dataType): request/reply.
func (o Opcode) unconnectedData() bool { return o == OpUReq || o == OpUReply }

// Addr is a RollCall logical address: net.unit.port, three big-endian uint16s.
//
// In a request, Dst is the target unit (a card behind the frame) and Src is the
// client; replies swap them. Observed in captures: units use Net=0 with
// Unit=0x10xx and a small Port; the client uses Net=0, Unit=0 and a small handle.
//
// NOTE: the exact mapping from the RollTrak CLI form `cmd@<net>:<addr>:<slot>`
// to these three fields is not yet confirmed against hardware — see README.
type Addr struct {
	Net  uint16
	Unit uint16
	Port uint16
}

func (a Addr) String() string {
	return fmt.Sprintf("%04x:%04x:%04x", a.Net, a.Unit, a.Port)
}

// UnitAddr builds the wire address of a unit reached via a controller at CLI
// address `addr`, in the given `slot`. This is the decoded mapping of the
// RollTrak CLI form cmd@<net>:<addr>:<slot>, confirmed against captures:
//
//	unit = (addr << 8) | slot,  net = 0,  port = 0
//
// Examples: a card in slot 5 behind a frame whose controller address is 0x12 →
// UnitAddr(0x12, 0x05) = unit 0x1205; a card addressed directly on its own IP
// uses controller address 0x30 → UnitAddr(0x30, 0x00) = unit 0x3000.
func UnitAddr(addr, slot uint8) Addr {
	return Addr{Net: 0, Unit: uint16(addr)<<8 | uint16(slot), Port: 0}
}

// ValueKind is the dataType field of a value body.
type ValueKind uint32

const (
	KindNone    ValueKind = 0          // no value present
	KindInt     ValueKind = 1          // integer, stored in the following uint32
	KindString  ValueKind = 2          // NUL-terminated ASCII after a reserved uint32
	KindUnknown ValueKind = 0xFFFFFFFF // a dataType we don't decode yet (see RawType/Raw)
)

// Value is a parameter value carried by a SET or REPLY message.
//
// Only int and string have been observed on the wire. Any other dataType is
// surfaced as KindUnknown (with the raw dataType in RawType and the undecoded
// bytes in Raw) rather than silently dropped, so callers can detect and report
// it instead of mistaking it for "no value".
type Value struct {
	Kind    ValueKind
	Int     uint32
	Str     string
	RawType uint32 // on-wire dataType when Kind==KindUnknown
	Raw     []byte // undecoded value payload when Kind==KindUnknown
}

// Int returns an integer Value.
func Int(v uint32) Value { return Value{Kind: KindInt, Int: v} }

// Str returns a string Value.
func Str(s string) Value { return Value{Kind: KindString, Str: s} }

func (v Value) String() string {
	switch v.Kind {
	case KindInt:
		return fmt.Sprintf("int(%d)", v.Int)
	case KindString:
		return fmt.Sprintf("str(%q)", v.Str)
	case KindUnknown:
		return fmt.Sprintf("unknown(type=%d, % x)", v.RawType, v.Raw)
	default:
		return "<none>"
	}
}

func unknownValue(dataType uint32, raw []byte) Value {
	return Value{Kind: KindUnknown, RawType: dataType, Raw: append([]byte(nil), raw...)}
}

// encode serialises the value body (dataType + payload).
func (v Value) encode() []byte {
	switch v.Kind {
	case KindInt:
		b := make([]byte, 8)
		binary.BigEndian.PutUint32(b[0:4], uint32(KindInt))
		binary.BigEndian.PutUint32(b[4:8], v.Int)
		return b
	case KindString:
		b := make([]byte, 8, 8+len(v.Str)+1)
		binary.BigEndian.PutUint32(b[0:4], uint32(KindString))
		// b[4:8] is a reserved uint32, left zero (matches observed captures).
		b = append(b, v.Str...)
		b = append(b, 0) // NUL terminator
		return b
	default:
		return nil
	}
}

// encodeUnconnected serialises a value in unconnected (RollTrak) form:
//
//	dataType(u16) | reserved(u32) | payload
//
// where an int payload is a uint32 and a string is NUL-terminated ASCII. This
// matches the observed unconnected REPLY layout. NOTE: no unconnected *write* was
// ever captured, so using this to SET is UNCONFIRMED — validate on a non-air
// frame (the blast verify-and-retry will flag it if the device rejects it).
func (v Value) encodeUnconnected() []byte {
	switch v.Kind {
	case KindInt:
		b := make([]byte, 0, 6)
		b = binary.BigEndian.AppendUint16(b, uint16(KindInt))
		b = binary.BigEndian.AppendUint32(b, v.Int) // value follows directly (no reserved)
		return b
	case KindString:
		b := make([]byte, 0, 6+len(v.Str)+1)
		b = binary.BigEndian.AppendUint16(b, uint16(KindString))
		b = binary.BigEndian.AppendUint32(b, 0) // reserved
		b = append(b, v.Str...)
		b = append(b, 0) // NUL terminator
		return b
	default:
		return nil
	}
}

// Message is a decoded RollCall message.
type Message struct {
	Dst    Addr
	Src    Addr
	Opcode Opcode
	Flags  byte
	CmdID  uint32 // valid for GET/SET/REPLY
	Value  Value  // Kind==KindNone when absent
	Raw    []byte // inner payload after opcode+flags, for non-data opcodes (e.g. IDENT name)
}

func putAddr(b []byte, a Addr) {
	binary.BigEndian.PutUint16(b[0:2], a.Net)
	binary.BigEndian.PutUint16(b[2:4], a.Unit)
	binary.BigEndian.PutUint16(b[4:6], a.Port)
}

func getAddr(b []byte) Addr {
	return Addr{
		Net:  binary.BigEndian.Uint16(b[0:2]),
		Unit: binary.BigEndian.Uint16(b[2:4]),
		Port: binary.BigEndian.Uint16(b[4:6]),
	}
}

// inner builds the inner payload (everything after the innerLen field).
func (m Message) inner() []byte {
	buf := []byte{byte(m.Opcode), m.Flags}
	switch {
	case m.Opcode.connectedData(): // uint32 command id
		buf = binary.BigEndian.AppendUint32(buf, m.CmdID)
		if m.Value.Kind != KindNone {
			buf = append(buf, m.Value.encode()...)
		}
	case m.Opcode.unconnectedData(): // uint16 command id
		if len(m.Raw) > 0 {
			buf = append(buf, m.Raw...) // caller pre-built the body (e.g. a custom SET opcode)
		} else {
			buf = binary.BigEndian.AppendUint16(buf, uint16(m.CmdID))
			if m.Value.Kind != KindNone {
				buf = append(buf, m.Value.encodeUnconnected()...)
			}
		}
	default:
		if len(m.Raw) > 0 {
			buf = append(buf, m.Raw...)
		}
	}
	return buf
}

// Encode serialises a complete on-wire message:
//
//	00 0C | outerLen(u16) | dst[6] | src[6] | innerLen(u16) | inner
func (m Message) Encode() []byte {
	inner := m.inner()
	body := make([]byte, 12+2+len(inner))
	putAddr(body[0:6], m.Dst)
	putAddr(body[6:12], m.Src)
	binary.BigEndian.PutUint16(body[12:14], uint16(len(inner)))
	copy(body[14:], inner)

	out := make([]byte, 4+len(body))
	out[0], out[1] = Magic[0], Magic[1]
	binary.BigEndian.PutUint16(out[2:4], uint16(len(body)))
	copy(out[4:], body)
	return out
}

var (
	// ErrShort means b does not yet contain a full message.
	ErrShort = errors.New("rollcall: short buffer")
	// ErrMagic means b does not start with the RollCall magic.
	ErrMagic = errors.New("rollcall: bad magic")
	// ErrMalformed means the framing was internally inconsistent.
	ErrMalformed = errors.New("rollcall: malformed message")
)

// Decode parses one message from the front of b. It returns the message and the
// number of bytes consumed. If b does not yet hold a complete message it returns
// ErrShort and the caller should read more bytes and retry.
func Decode(b []byte) (Message, int, error) {
	if len(b) < 4 {
		return Message{}, 0, ErrShort
	}
	if b[0] != Magic[0] || b[1] != Magic[1] {
		return Message{}, 0, ErrMagic
	}
	outer := int(binary.BigEndian.Uint16(b[2:4]))
	if outer < 14 {
		return Message{}, 0, ErrMalformed
	}
	if len(b) < 4+outer {
		return Message{}, 0, ErrShort
	}
	body := b[4 : 4+outer]

	var m Message
	m.Dst = getAddr(body[0:6])
	m.Src = getAddr(body[6:12])
	innerLen := int(binary.BigEndian.Uint16(body[12:14]))
	if 14+innerLen > len(body) || innerLen < 1 {
		return Message{}, 0, ErrMalformed
	}
	inner := body[14 : 14+innerLen]

	m.Opcode = Opcode(inner[0])
	if innerLen >= 2 {
		m.Flags = inner[1]
	}

	switch {
	case m.Opcode.connectedData() && innerLen >= 6:
		m.CmdID = binary.BigEndian.Uint32(inner[2:6])
		m.Value = decodeConnectedValue(inner[6:])
	case m.Opcode.unconnectedData() && innerLen >= 4:
		m.CmdID = uint32(binary.BigEndian.Uint16(inner[2:4]))
		m.Value = decodeUnconnectedValue(inner[4:])
	default:
		if innerLen > 2 {
			m.Raw = append([]byte(nil), inner[2:]...)
		}
	}

	return m, 4 + outer, nil
}

// decodeConnectedValue parses a connected-mode value body: dataType(u32) then
// the payload (int = u32; string = reserved u32 + NUL ASCII).
func decodeConnectedValue(rest []byte) Value {
	if len(rest) < 4 {
		return Value{}
	}
	dataType := binary.BigEndian.Uint32(rest[0:4])
	switch ValueKind(dataType) {
	case KindInt:
		if len(rest) >= 8 {
			return Value{Kind: KindInt, Int: binary.BigEndian.Uint32(rest[4:8])}
		}
	case KindString:
		if len(rest) >= 8 {
			return Value{Kind: KindString, Str: cstr(rest[8:])}
		}
	}
	return unknownValue(dataType, rest[4:]) // float/enum/truncated — surface, don't drop
}

// decodeUnconnectedValue parses an unconnected-mode value body. Confirmed against
// captures + hardware:
//
//	int   (dataType 1): dataType(u16) | value          — NO reserved word, like
//	                    connected int (value is u32, or u16 for a 2-byte body)
//	string(dataType 2/3): dataType(u16) | reserved(u32) | NUL-terminated ASCII
//
// dataType 3 is the "No Unit Fitted" status string (an empty/absent slot). Enum/
// select parameters are integers, so getting the int layout right is what makes
// them read (they showed "undefined" when int was mis-parsed as having a reserved).
func decodeUnconnectedValue(rest []byte) Value {
	if len(rest) < 2 {
		return Value{}
	}
	dataType := uint32(binary.BigEndian.Uint16(rest[0:2]))
	body := rest[2:]
	switch dataType {
	case 1: // int — value immediately follows the dataType (no reserved word)
		switch {
		case len(body) >= 4:
			return Value{Kind: KindInt, Int: binary.BigEndian.Uint32(body[0:4])}
		case len(body) >= 2:
			return Value{Kind: KindInt, Int: uint32(binary.BigEndian.Uint16(body[0:2]))}
		}
	case 2, 3: // string — a reserved u32 then NUL-terminated ASCII
		if len(body) >= 4 {
			return Value{Kind: KindString, Str: cstr(body[4:])}
		}
	}
	return unknownValue(dataType, body)
}

// cstr returns the bytes up to the first NUL as a string.
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

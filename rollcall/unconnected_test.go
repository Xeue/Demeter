package rollcall

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"
	"time"
)

// TestClientUnconnectedGetEndToEnd drives the mode-aware Client over a pipe: it
// must send the 0x15 login, issue an OpUReq (0x0b) GET, and route the OpUReply
// (0x0c) back to the caller.
func TestClientUnconnectedGetEndToEnd(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	sawLogin := make(chan struct{}, 1)
	go func() {
		defer srvConn.Close()
		br := bufio.NewReader(srvConn)
		for {
			req, err := readMessage(br)
			if err != nil {
				return
			}
			switch req.Opcode {
			case OpLogin:
				select {
				case sawLogin <- struct{}{}:
				default:
				}
			case OpUReq:
				reply := Message{Dst: req.Src, Src: req.Dst, Opcode: OpUReply, CmdID: req.CmdID, Value: Str("10.0.0.1")}
				if _, err := srvConn.Write(reply.Encode()); err != nil {
					return
				}
			}
		}
	}()

	c := NewConn(cliConn, WithMode(Unconnected))
	defer c.Close()

	select {
	case <-sawLogin:
	case <-time.After(time.Second):
		t.Fatal("client did not send the unconnected 0x15 login")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := c.Get(ctx, UnitAddr(0x12, 0x05), 4101)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Kind != KindString || v.Str != "10.0.0.1" {
		t.Errorf("got %s, want str(10.0.0.1)", v)
	}
}

// frame wraps a decoded body (dst+src+innerLen+inner) in the 00 0c + outerLen
// header to form a complete on-wire message.
func frame(bodyHex string) []byte {
	body, err := hex.DecodeString(bodyHex)
	if err != nil {
		panic(err)
	}
	out := []byte{0x00, 0x0c}
	out = binary.BigEndian.AppendUint16(out, uint16(len(body)))
	return append(out, body...)
}

// The following byte strings are the exact bodies lifted from the RollTrak
// (unconnected) capture "Capture for sam.pcapng".

// TestUnconnectedFrameAddressReply replicates the hardware (rcprobe against
// 10.40.128.10): a GET to the self address unit 0x0000 is answered by the real
// controller (unit 0x1200), so reply.Src != request.Dst. The client must still
// route it back to the caller (the frame-address discovery the app does first).
func TestUnconnectedFrameAddressReply(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	go func() {
		defer srvConn.Close()
		br := bufio.NewReader(srvConn)
		for {
			req, err := readMessage(br)
			if err != nil {
				return
			}
			if req.Opcode != OpUReq {
				continue
			}
			reply := Message{
				Dst:    req.Src,
				Src:    Addr{Net: 0, Unit: 0x1200, Port: 0}, // controller, not the requested 0x0000
				Opcode: OpUReply, CmdID: req.CmdID, Value: Str("Unit Addr = 0x12"),
			}
			srvConn.Write(reply.Encode())
		}
	}()
	c := NewConn(cliConn, WithMode(Unconnected))
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	v, err := c.Get(ctx, UnitAddr(0x00, 0x00), 17044)
	if err != nil {
		t.Fatalf("Get unit-0 (frame address): %v", err)
	}
	if v.Kind != KindString || v.Str != "Unit Addr = 0x12" {
		t.Errorf("got %s, want str(Unit Addr = 0x12)", v)
	}
}

// TestClientUnconnectedSetEchoes: a unconnected SET (best-guess opcode 0x0b with
// a value body) round-trips — the fake frame echoes the value in a 0x0c reply and
// Set returns it (this is what the blast verify-and-retry checks against).
func TestClientUnconnectedSetEchoes(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	go func() {
		defer srvConn.Close()
		br := bufio.NewReader(srvConn)
		for {
			req, err := readMessage(br)
			if err != nil {
				return
			}
			if req.Opcode != OpUReq {
				continue // ignore the login
			}
			// A SET carries a value (Kind != None); echo it back as a 0x0c reply.
			reply := Message{Dst: req.Src, Src: req.Dst, Opcode: OpUReply, CmdID: req.CmdID, Value: req.Value}
			if _, err := srvConn.Write(reply.Encode()); err != nil {
				return
			}
		}
	}()

	c := NewConn(cliConn, WithMode(Unconnected))
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got, err := c.Set(ctx, UnitAddr(0x12, 0x05), 4101, Str("10.0.0.99"))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got.Kind != KindString || got.Str != "10.0.0.99" {
		t.Errorf("echoed value = %s, want str(10.0.0.99)", got)
	}
}

// TestUnconnectedSetCustomOpcode: the SET opcode is configurable for hardware
// trial-and-error — the encoded first inner byte must be the chosen opcode, with
// the value body unchanged.
func TestUnconnectedSetCustomOpcode(t *testing.T) {
	body := binary.BigEndian.AppendUint16(nil, uint16(4101))
	body = append(body, Str("x").encodeUnconnected()...)
	got := Message{Dst: UnitAddr(0x12, 0x05), Src: Addr{Port: 0x00ff}, Opcode: 0x0d, Raw: body}.Encode()
	// inner starts at offset 4(header)+12(addrs)+2(innerLen) = 18.
	if got[18] != 0x0d {
		t.Errorf("inner opcode = 0x%02x, want 0x0d", got[18])
	}
	// cmd (u16) follows opcode+flags: bytes 20-21 = 0x1005.
	if got[20] != 0x10 || got[21] != 0x05 {
		t.Errorf("cmd bytes = %02x%02x, want 1005", got[20], got[21])
	}
}

// TestUnconnectedLoginBytes: our 0x15 login matches the captured broadcast login.
func TestUnconnectedLoginBytes(t *testing.T) {
	want := frame("0000000000ff0000000000010003150000")
	got := Message{
		Dst:    Addr{Net: 0, Unit: 0, Port: 0x00ff},
		Src:    Addr{Net: 0, Unit: 0, Port: 0x0001},
		Opcode: OpLogin,
		Raw:    []byte{0x00},
	}.Encode()
	if !bytes.Equal(got, want) {
		t.Errorf("login bytes:\n got  %x\n want %x", got, want)
	}
}

// TestUnconnectedGetBytes: a unconnected GET of cmd 4101 from unit 0x3000 matches
// the captured request (opcode 0x0b, uint16 cmd, port-0 address).
func TestUnconnectedGetBytes(t *testing.T) {
	want := frame("0000300000000000000000ff00040b001005")
	got := Message{
		Dst:    UnitAddr(0x30, 0x00), // unit 0x3000, port 0
		Src:    Addr{Net: 0, Unit: 0, Port: 0x00ff},
		Opcode: OpUReq,
		CmdID:  4101, // 0x1005
	}.Encode()
	if !bytes.Equal(got, want) {
		t.Errorf("unconnected GET bytes:\n got  %x\n want %x", got, want)
	}
}

// TestUnconnectedReplyDecode: a string reply (0x0c) decodes to the right cmd,
// unit and value.
func TestUnconnectedReplyDecode(t *testing.T) {
	m, _, err := Decode(frame("0000000000ff00003000000000180c00100500020000000031302e3130302e3132382e313400"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Opcode != OpUReply {
		t.Errorf("opcode = %s, want OpUReply", m.Opcode)
	}
	if m.CmdID != 4101 {
		t.Errorf("cmd = %d, want 4101", m.CmdID)
	}
	if m.Src != (Addr{Net: 0, Unit: 0x3000, Port: 0}) {
		t.Errorf("src = %s, want 0000:3000:0000", m.Src)
	}
	if m.Value.Kind != KindString || m.Value.Str != "10.100.128.14" {
		t.Errorf("value = %s, want str(10.100.128.14)", m.Value)
	}
}

// TestUnconnectedIntReply: an integer reply (dataType 1) — e.g. a select/enum
// index like Reference Source — has NO reserved word (value follows dataType
// directly). Getting this right is what makes the select fields read instead of
// showing "undefined".
func TestUnconnectedIntReply(t *testing.T) {
	// 0x0c reply: cmd 4501 (Reference Source), dataType 1 (int), value 1 ("PTP").
	body := []byte{0x0c, 0x00}
	body = binary.BigEndian.AppendUint16(body, 4501) // cmd
	body = binary.BigEndian.AppendUint16(body, 1)    // dataType = int
	body = binary.BigEndian.AppendUint32(body, 1)    // value (no reserved)
	full := encodeControl(Addr{Port: 0x00ff}, Addr{Unit: 0x1205}, body)
	dm, _, err := Decode(full)
	if err != nil {
		t.Fatal(err)
	}
	if dm.CmdID != 4501 {
		t.Errorf("cmd = %d, want 4501", dm.CmdID)
	}
	if dm.Value.Kind != KindInt || dm.Value.Int != 1 {
		t.Errorf("value = %s, want int(1)", dm.Value)
	}
}

// encodeControl frames an arbitrary inner payload with dst/src.
func encodeControl(dst, src Addr, inner []byte) []byte {
	b := make([]byte, 6+6+2)
	putAddr(b[0:6], dst)
	putAddr(b[6:12], src)
	binary.BigEndian.PutUint16(b[12:14], uint16(len(inner)))
	b = append(b, inner...)
	out := []byte{0x00, 0x0c}
	out = binary.BigEndian.AppendUint16(out, uint16(len(b)))
	return append(out, b...)
}

// TestUnconnectedNoUnitFitted: dataType 3 ("No Unit Fitted") — an absent slot —
// decodes as the status string so the scan treats it as not-a-UCP.
func TestUnconnectedNoUnitFitted(t *testing.T) {
	m, _, err := Decode(frame("0000000000ff00001200000000190c0040930003000000004e6f20556e69742046697474656400"))
	if err != nil {
		t.Fatal(err)
	}
	if m.CmdID != 16531 { // 0x4093 = 16530 + slot 1
		t.Errorf("cmd = %d, want 16531", m.CmdID)
	}
	if m.Value.Kind != KindString || m.Value.Str != "No Unit Fitted" {
		t.Errorf("value = %s, want str(No Unit Fitted)", m.Value)
	}
}

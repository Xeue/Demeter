// Command rcprobe is a RollCall connectivity probe. It dials a frame and reports
// exactly what it speaks, to diagnose "Cannot reach frame" / "connected but not
// responding" against real hardware (docs/ROLLCALL_HANDOVER.md, Task 0).
//
// Built to be dead-simple on a remote Windows box:
//   - double-click it (or run with no args) and it PROMPTS for the frame IP;
//   - it writes a results .txt next to the .exe — just send me that file;
//   - it tries the alternate port (2051) automatically if 2050 refuses;
//   - it pauses at the end so the window doesn't disappear.
//
// Or with flags:  rcprobe -frame 10.40.128.10 [-port 2050] [-addr 12] [-slot 05]
//
// Read-only (GETs only) — safe against a live frame. The most important output is
// PHASE 1: after the unconnected 0x15 login, the frame should announce its units
// via 0x14 IDENT — those announcements reveal the real addressing.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Xeue/Demeter/rollcall"
)

var out io.Writer = os.Stdout

var cReplies, cUReplies, cAcks, cIdents, cNacks, cOther atomic.Int64

func main() {
	frame := flag.String("frame", "", "frame IP (prompted if omitted)")
	port := flag.Int("port", 2050, "TCP port (2051 tried automatically if refused)")
	addrTok := flag.String("addr", "12", "card controller address, hex (e.g. 12)")
	slotTok := flag.String("slot", "05", "card slot, hex (e.g. 05)")
	cmd := flag.Uint("cmd", 4101, "command id to GET (4101=Ethernet-1 IP)")
	settle := flag.Duration("settle", 1200*time.Millisecond, "wait after each probe for a reply")
	flag.Parse()

	interactive := false
	if *frame == "" {
		interactive = true
		*frame = prompt("Enter the frame IP address (e.g. 10.40.128.10): ")
		if *frame == "" {
			fmt.Fprintln(os.Stderr, "no frame IP given")
			pauseIf(true)
			os.Exit(2)
		}
		if v := prompt("TCP port [2050]: "); v != "" {
			if p, err := strconv.Atoi(v); err == nil {
				*port = p
			}
		}
	}

	resultsPath := resultsFilePath(*frame)
	if f, err := os.Create(resultsPath); err == nil {
		defer f.Close()
		out = io.MultiWriter(os.Stdout, f)
	}

	logf("rcprobe — RollCall connectivity probe")
	logf("results file: %s", resultsPath)
	logf("==> When this finishes, SEND THE FILE ABOVE. <==")

	addr := mustHexByte(*addrTok, "addr")
	slot := mustHexByte(*slotTok, "slot")

	conn := dial(*frame, *port)
	if conn == nil {
		logf("\nRESULT: no TCP connection on any tried port — network/port/firewall, not protocol.")
		pauseIf(interactive)
		os.Exit(1)
	}
	defer conn.Close()
	go reader(conn)

	phase("0", "passive listen 2s — does the frame send anything on connect (before any login)?")
	time.Sleep(2 * time.Second)

	phase("1", "UNCONNECTED login (0x15), then listen 3s — the frame should announce its units via 0x14 IDENT (this reveals the real addressing)")
	send(conn, "LOGIN 0x15", login15())
	time.Sleep(3 * time.Second)

	phase("2", "UNCONNECTED GET 17044 at unit 0x0000 (Demeter's frame-address read — the FAILING one)")
	send(conn, "GET frame", unconnectedGet(rollcall.UnitAddr(0x00, 0x00), self, 17044))
	time.Sleep(*settle)

	phase("3", "UNCONNECTED card-type (16530) SWEEP across controller addresses — find which one the frame answers on")
	for _, a := range []uint8{0x00, 0x01, 0x02, 0x10, 0x12, 0x20, 0x30} {
		u := rollcall.UnitAddr(a, 0x00)
		send(conn, fmt.Sprintf("GET a=%02x", a), unconnectedGet(u, self, 16530))
		time.Sleep(500 * time.Millisecond)
	}

	phase("4", "UNCONNECTED GET cmd=%d at addr=%02x slot=%02x (your -addr/-slot)", *cmd, addr, slot)
	send(conn, "GET card", unconnectedGet(rollcall.UnitAddr(addr, slot), self, uint32(*cmd)))
	time.Sleep(*settle)

	phase("5", "CONNECTED fallback: IDENTITY 0x21 then GET 17044 at unit 0x0000")
	send(conn, "IDENT 0x21", identitySelf())
	time.Sleep(*settle)
	send(conn, "GET frame", rollcall.Message{Dst: rollcall.UnitAddr(0x00, 0x00), Src: rollcall.Addr{Port: 2}, Opcode: rollcall.OpGet, CmdID: 17044}.Encode())
	time.Sleep(*settle)

	logf("\n================ SUMMARY ================")
	logf("inbound: UNCONNECTED-reply(0x0c)=%d  connected-reply(0x47)=%d  IDENT=%d  ACK=%d  NACK=%d  other=%d",
		cUReplies.Load(), cReplies.Load(), cIdents.Load(), cNacks.Load(), cAcks.Load(), cOther.Load())
	switch {
	case cUReplies.Load() > 0:
		logf("The frame ANSWERS UNCONNECTED reads — see which PHASE/address got the 0x0c reply; that's the addressing to use.")
	case cIdents.Load() > 0:
		logf("The frame announced units via IDENT (PHASE 1) but didn't answer the GETs — the IDENT src addresses show the real units to read.")
	case cReplies.Load() > 0:
		logf("The frame answers CONNECTED reads (PHASE 5) but not unconnected — we may need connected mode + its addressing.")
	default:
		logf("The frame sent NOTHING back to login or any GET. Note the port, and whether anything appeared in PHASE 0/1.")
	}
	logf("\n==> DONE. Please send this file: %s", resultsPath)
	pauseIf(interactive)
}

// self is the unconnected client source seen in the capture.
var self = rollcall.Addr{Net: 0, Unit: 0, Port: 0x00ff}

// login15 is the unconnected broadcast login (inner: 15 00 00).
func login15() []byte {
	return rollcall.Message{
		Dst: rollcall.Addr{Port: 0x00ff}, Src: rollcall.Addr{Port: 0x0001},
		Opcode: rollcall.OpLogin, Raw: []byte{0x00},
	}.Encode()
}

// identitySelf is the connected-mode login replayed from the capture.
func identitySelf() []byte {
	body := []byte{
		0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xf4,
		0x04, 0x15, 0x20, 0x05,
		'C', 'o', 'n', 't', 'r', 'o', 'l', 'P', 'a', 'n', 'e', 'l',
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08,
	}
	return rollcall.Message{
		Dst: rollcall.Addr{Port: 0x00ff}, Src: rollcall.Addr{Port: 0x00ff},
		Opcode: rollcall.OpIdentSelf, Flags: 0x40, Raw: body,
	}.Encode()
}

// unconnectedGet frames an unconnected-mode read (inner: 0b 00 <cmd u16>).
func unconnectedGet(dst, src rollcall.Addr, cmd uint32) []byte {
	inner := []byte{0x0b, 0x00, byte(cmd >> 8), byte(cmd)}
	body := append(addrBytes(dst), addrBytes(src)...)
	body = binary.BigEndian.AppendUint16(body, uint16(len(inner)))
	body = append(body, inner...)
	out := []byte{0x00, 0x0c}
	out = binary.BigEndian.AppendUint16(out, uint16(len(body)))
	return append(out, body...)
}

func addrBytes(a rollcall.Addr) []byte {
	b := make([]byte, 6)
	binary.BigEndian.PutUint16(b[0:2], a.Net)
	binary.BigEndian.PutUint16(b[2:4], a.Unit)
	binary.BigEndian.PutUint16(b[4:6], a.Port)
	return b
}

func dial(host string, port int) net.Conn {
	tryPorts := []int{port}
	if port == 2050 {
		tryPorts = append(tryPorts, 2051)
	}
	for _, p := range tryPorts {
		addr := net.JoinHostPort(host, strconv.Itoa(p))
		logf("connecting to %s ...", addr)
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			logf("  TCP connect FAILED: %v", err)
			continue
		}
		logf("  TCP connected OK (%s -> %s)", conn.LocalAddr(), conn.RemoteAddr())
		return conn
	}
	return nil
}

func send(conn net.Conn, label string, msg []byte) {
	logf("  -> SEND %-10s %s", label, hex.EncodeToString(msg))
	if _, err := conn.Write(msg); err != nil {
		logf("  -> SEND error: %v", err)
	}
}

func reader(conn net.Conn) {
	hdr := make([]byte, 4)
	for {
		if _, err := readFull(conn, hdr); err != nil {
			return
		}
		if hdr[0] != 0x00 || hdr[1] != 0x0c {
			logf("  <- RECV non-RollCall: %s", hex.EncodeToString(hdr))
			cOther.Add(1)
			continue
		}
		full := make([]byte, 4+int(binary.BigEndian.Uint16(hdr[2:4])))
		copy(full, hdr)
		if _, err := readFull(conn, full[4:]); err != nil {
			return
		}
		m, _, err := rollcall.Decode(full)
		if err != nil {
			logf("  <- RECV %s  (decode err: %v)", hex.EncodeToString(full), err)
			cOther.Add(1)
			continue
		}
		count(m.Opcode)
		logf("  <- RECV %s  | %s flags=0x%02x cmd=%d src=%s val=%s ascii=%q",
			hex.EncodeToString(full), m.Opcode, m.Flags, m.CmdID, m.Src, m.Value, asciiOf(full))
	}
}

func count(op rollcall.Opcode) {
	switch op {
	case rollcall.OpReply:
		cReplies.Add(1)
	case rollcall.OpUReply:
		cUReplies.Add(1)
	case rollcall.OpAck:
		cAcks.Add(1)
	case rollcall.OpIdentUnit, rollcall.OpIdentSelf:
		cIdents.Add(1)
	case rollcall.OpNack:
		cNacks.Add(1)
	default:
		cOther.Add(1)
	}
}

// asciiOf returns the longest printable-ASCII run in b (surfaces IDENT unit names).
func asciiOf(b []byte) string {
	best, cur := "", make([]byte, 0, 32)
	flush := func() {
		if len(cur) > len(best) {
			best = string(cur)
		}
		cur = cur[:0]
	}
	for _, c := range b {
		if c >= 32 && c < 127 {
			cur = append(cur, c)
		} else {
			flush()
		}
	}
	flush()
	if len(best) < 3 {
		return ""
	}
	return best
}

func readFull(conn net.Conn, b []byte) (int, error) {
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	n := 0
	for n < len(b) {
		k, err := conn.Read(b[n:])
		if err != nil {
			return n, err
		}
		n += k
	}
	return n, nil
}

func phase(n, format string, args ...any) {
	logf("\n=== PHASE %s: %s", n, fmt.Sprintf(format, args...))
}

func logf(format string, args ...any) {
	fmt.Fprintf(out, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

func resultsFilePath(frame string) string {
	name := fmt.Sprintf("rcprobe-%s-%s.txt", strings.ReplaceAll(frame, ":", "_"), time.Now().Format("20060102-150405"))
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), name)
	}
	return name
}

func prompt(msg string) string {
	fmt.Print(msg)
	s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(s)
}

func pauseIf(interactive bool) {
	if !interactive {
		return
	}
	fmt.Print("\nPress Enter to close...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func mustHexByte(tok, name string) uint8 {
	v, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad -%s %q (want hex like 12)\n", name, tok)
		os.Exit(2)
	}
	return uint8(v)
}

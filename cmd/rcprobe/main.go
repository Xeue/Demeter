// Command rcprobe is a RollCall connectivity probe. It dials a frame and reports
// exactly what it speaks, to diagnose "Cannot reach frame" / "connected but not
// responding" against real hardware (see docs/ROLLCALL_PROTOCOL.md, Further work).
//
// Built to be dead-simple on a remote Windows box:
//   - double-click it (or run with no args) and it PROMPTS for the frame IP;
//   - it writes a results .txt next to the .exe - just send me that file;
//   - it tries the alternate port (2051) automatically if 2050 refuses;
//   - it pauses at the end so the window doesn't disappear.
//
// Or with flags:  rcprobe -frame 10.40.128.10 [-port 2050] [-addr 12] [-slot 05]
//
// Read-only (GETs only), safe against a live frame. The most important output is
// PHASE 1: after the unconnected 0x15 login, the frame should announce its units
// via 0x14 IDENT - those announcements reveal the real addressing.
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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	capture := flag.Bool("capture", false, "write-capture proxy: relay a known-good writer (rolltrak) to the frame and decode the SET it sends")
	listen := flag.String("listen", ":2050", "capture mode: local address to listen on (point rolltrak's -a at this host)")
	runCmd := flag.String("run", "", "capture mode: optional rolltrak command to auto-run THROUGH the proxy, e.g. \"4114@0000:1205:05=1\"")
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

	logf("rcprobe - RollCall connectivity probe")
	logf("results file: %s", resultsPath)
	logf("==> When this finishes, SEND THE FILE ABOVE. <==")

	if *capture {
		runCapture(*frame, *port, *listen, *runCmd, interactive)
		return
	}

	addr := mustHexByte(*addrTok, "addr")
	slot := mustHexByte(*slotTok, "slot")

	conn := dial(*frame, *port)
	if conn == nil {
		logf("\nRESULT: no TCP connection on any tried port - network/port/firewall, not protocol.")
		pauseIf(interactive)
		os.Exit(1)
	}
	defer conn.Close()
	go reader(conn)

	phase("0", "passive listen 2s - does the frame send anything on connect (before any login)?")
	time.Sleep(2 * time.Second)

	phase("1", "UNCONNECTED login (0x15), then listen 3s - the frame should announce its units via 0x14 IDENT (this reveals the real addressing)")
	send(conn, "LOGIN 0x15", login15())
	time.Sleep(3 * time.Second)

	phase("2", "UNCONNECTED GET 17044 at unit 0x0000 (Demeter's frame-address read - the FAILING one)")
	send(conn, "GET frame", unconnectedGet(rollcall.UnitAddr(0x00, 0x00), self, 17044))
	time.Sleep(*settle)

	phase("3", "UNCONNECTED card-type (16530) SWEEP across controller addresses - find which one the frame answers on")
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
		logf("The frame ANSWERS UNCONNECTED reads - see which PHASE/address got the 0x0c reply; that's the addressing to use.")
	case cIdents.Load() > 0:
		logf("The frame announced units via IDENT (PHASE 1) but didn't answer the GETs - the IDENT src addresses show the real units to read.")
	case cReplies.Load() > 0:
		logf("The frame answers CONNECTED reads (PHASE 5) but not unconnected - we may need connected mode + its addressing.")
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

var logMu sync.Mutex

func logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
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

// ============================ write-capture mode ============================
//
// We have a KNOWN-GOOD unconnected writer for these exact frames: the legacy app
// programmed cards with `rolltrak -a <frameIP> <cmd>@0000:<addr>:<slot>=<value>`.
// Rather than guess the SET wire format (or blind-probe opcodes against live
// broadcast hardware), point rolltrak at this transparent proxy instead of the
// frame (`rolltrak -a <thisHost> ...`). The proxy relays every byte to the real
// frame, so the write genuinely happens (you can confirm the value changed), while
// decoding each message and flagging the SET with its exact opcode + body. No
// Wireshark / npcap needed. Send me the results file and I implement it byte-for-byte.

func runCapture(frameIP string, port int, listen, run string, interactive bool) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		logf("CAPTURE: cannot listen on %s: %v", listen, err)
		pauseIf(interactive)
		os.Exit(1)
	}
	defer ln.Close()

	host := listenHost(listen)
	logf("\n================ WRITE-CAPTURE (transparent proxy) ================")
	logf("listening on %s  ->  relaying to frame %s:%d", listen, frameIP, port)
	logf("Now drive ONE write through the proxy with the known-good writer:")
	logf("    rolltrak -a %s <cmd>@0000:<addr>:<slot>=<value>", host)
	logf("  e.g.  rolltrak -a %s 4114@0000:1205:05=1", host)
	logf("Pick a SAFE, reversible parameter on a NON-AIR card. The SET will be")
	logf("printed below as 'WRITE CAPTURED' with its exact bytes. Ctrl-C when done.")
	logf("===================================================================\n")

	if run != "" {
		go func() {
			time.Sleep(750 * time.Millisecond) // let the listener settle
			args := append([]string{"-a", host}, strings.Fields(run)...)
			logf("CAPTURE: auto-running: rolltrak %s", strings.Join(args, " "))
			c := exec.Command("rolltrak", args...)
			c.Stdout, c.Stderr = out, out
			if err := c.Run(); err != nil {
				logf("CAPTURE: rolltrak run error: %v (is rolltrak on PATH?)", err)
			}
		}()
	}

	for {
		client, err := ln.Accept()
		if err != nil {
			logf("CAPTURE: accept error: %v", err)
			pauseIf(interactive)
			return
		}
		go handleCapture(client, frameIP, port)
	}
}

// handleCapture bridges one accepted writer connection to the real frame and
// relays in both directions, decoding as it goes.
func handleCapture(client net.Conn, frameIP string, port int) {
	defer client.Close()
	frame := dial(frameIP, port)
	if frame == nil {
		logf("CAPTURE: cannot reach frame %s:%d - is it the right IP/port?", frameIP, port)
		return
	}
	defer frame.Close()
	logf("CAPTURE: writer connected from %s; relaying to %s", client.RemoteAddr(), frame.RemoteAddr())
	done := make(chan struct{}, 2)
	go func() { pump(true, client, frame); done <- struct{}{} }()  // writer -> frame (the interesting direction)
	go func() { pump(false, frame, client); done <- struct{}{} }() // frame -> writer (replies)
	<-done                                                         // first half to close tears the pair down
}

// pump relays one direction frame-by-frame: it reads a whole RollCall message,
// forwards the exact bytes (so the stream is byte-faithful), then decodes it for
// the log. Writes from the client side are dissected in full.
func pump(fromClient bool, src, dst net.Conn) {
	hdr := make([]byte, 4)
	for {
		if _, err := io.ReadFull(src, hdr); err != nil { // no deadline: relayed sessions are long-lived
			return
		}
		if hdr[0] != rollcall.Magic[0] || hdr[1] != rollcall.Magic[1] {
			_, _ = dst.Write(hdr) // not RollCall framing - forward verbatim, keep going
			continue
		}
		full := make([]byte, 4+int(binary.BigEndian.Uint16(hdr[2:4])))
		copy(full, hdr)
		if _, err := io.ReadFull(src, full[4:]); err != nil {
			return
		}
		if _, err := dst.Write(full); err != nil { // forward first so timing is unaffected
			return
		}
		m, _, err := rollcall.Decode(full)
		seq := capSeq.Add(1)
		dir := "frame->writer"
		if fromClient {
			dir = "writer->frame"
		}
		if err != nil {
			logf("  #%03d [%s] RAW %s (decode err: %v)", seq, dir, hex.EncodeToString(full), err)
			continue
		}
		if fromClient && isWrite(m) {
			logWrite(seq, full, m)
			continue
		}
		// Full hex on every frame so the whole session can be diffed against
		// Demeter's wire trace (ROLLCALL_WIRETRACE), not just the writes.
		logf("  #%03d [%s] %-10s flags=0x%02x dst=%s src=%s cmd=%d val=%s | %s",
			seq, dir, m.Opcode, m.Flags, m.Dst, m.Src, m.CmdID, m.Value, hex.EncodeToString(full))
	}
}

// capSeq numbers every relayed frame so the two directions interleave readably.
var capSeq atomic.Int64

// isWrite reports whether a client->frame message looks like a parameter write
// (so we dissect it). It deliberately catches the case where the SET reuses the
// READ opcode (0x0b) but carries a value body, as well as any distinct/unknown
// write opcode - while ignoring logins, idents, opens, acks and plain reads.
func isWrite(m rollcall.Message) bool {
	switch m.Opcode {
	case rollcall.OpLogin, rollcall.OpIdentSelf, rollcall.OpIdentUnit, rollcall.OpOpen, rollcall.OpAck, rollcall.OpNack:
		return false
	case rollcall.OpUReq, rollcall.OpGet:
		return m.Value.Kind != rollcall.KindNone // a "read" carrying a value IS a write
	default:
		return true // any other opcode from the writer is a write candidate
	}
}

// logWrite dissects a captured write straight from the raw frame (not relying on
// Decode, which won't parse an unknown opcode), under both the unconnected (u16
// command id) and connected (u32) interpretations, and states the verdict against
// Demeter's current SET encoding.
func logWrite(seq int64, full []byte, m rollcall.Message) {
	logf("")
	logf("  ******************** #%03d WRITE CAPTURED (writer -> frame) ********************", seq)
	logf("  full frame: %s", hex.EncodeToString(full))
	logf("  dst=%s  src=%s", m.Dst, m.Src)
	if len(full) < 18 {
		logf("  (frame too short to dissect)")
		logf("  *************************************************************************")
		return
	}
	innerLen := int(binary.BigEndian.Uint16(full[16:18]))
	if 18+innerLen > len(full) {
		innerLen = len(full) - 18
	}
	inner := full[18 : 18+innerLen]
	op := inner[0]
	var flags byte
	if len(inner) >= 2 {
		flags = inner[1]
	}
	body := inner[2:] // payload after opcode + flags
	logf("  opcode = 0x%02x   flags = 0x%02x", op, flags)
	logf("  payload (after opcode+flags): %s", hex.EncodeToString(body))
	if len(body) >= 2 {
		cmd16 := binary.BigEndian.Uint16(body[0:2])
		rest := body[2:]
		logf("  as UNCONNECTED: cmd(u16)=%d (0x%04x)  value-body=%s", cmd16, cmd16, hex.EncodeToString(rest))
		if len(rest) >= 2 {
			dt := binary.BigEndian.Uint16(rest[0:2])
			logf("                  value: dataType(u16)=%d  payload=%s", dt, hex.EncodeToString(rest[2:]))
		}
	}
	if len(body) >= 4 {
		cmd32 := binary.BigEndian.Uint32(body[0:4])
		logf("  as CONNECTED:   cmd(u32)=%d (0x%08x)  value-body=%s", cmd32, cmd32, hex.EncodeToString(body[4:]))
	}
	logf("  VERDICT: Demeter currently sends an unconnected SET as opcode 0x0b with")
	logf("           body [cmd(u16) | dataType(u16) | value]  (a best guess).")
	if op != byte(rollcall.OpUReq) {
		logf("           >> The real write opcode is 0x%02x, NOT 0x0b. Set config", op)
		logf("           >> rollcallSetOpcode=\"%02x\" and/or fix rollcall.Client.Set.", op)
	} else {
		logf("           >> Opcode matches 0x0b; compare the value-body layout above")
		logf("           >> against encodeUnconnected to confirm the body is right.")
	}
	logf("  *************************************************************************")
	logf("")
}

// listenHost returns the host to tell rolltrak to point -a at (a wildcard listen
// address maps to loopback for the same-box case).
func listenHost(listen string) string {
	h, _, err := net.SplitHostPort(listen)
	if err != nil || h == "" || h == "0.0.0.0" || h == "::" {
		return "127.0.0.1"
	}
	return h
}

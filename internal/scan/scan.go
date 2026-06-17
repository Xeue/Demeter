// Package scan is a faithful Go port of Demeter's device scan + blast logic
// (main.ts checkFrame/getFrameAddress/checkCard/computeGroupCommands/doCommands,
// lines ~448-959). It talks to frames through device.Device connections (one per
// IP, supplied by a Conns provider) and emits UI events through an Events sink.
//
// CheckFrame mutates the *copy* of a frame handed to it (the caller — the
// per-frame actor — gives it an exclusive snapshot and later merges only the
// scanned fields back, so concurrent operator edits to prefered/enabled are not
// lost). Comparisons go through model.ValuesEqualLoose so an already-correct
// value is not re-blasted every cycle.
package scan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
)

// Events receives scan progress and results for fan-out to the UI.
type Events interface {
	FrameStatus(frameIP, status string, offline bool)
	SlotInfo(frameIP string, frame *model.Frame, slotName string, slot *model.Slot)
	FrameError(frameIP, errMsg string)
}

// Conns supplies a device connection for an IP (frame IP or a card's own IP),
// reusing a persistent connection across cycles. Implemented by the actor.
type Conns interface {
	Device(ctx context.Context, ip string) (device.Device, error)
}

// Scanner holds the shared, frame-independent scan dependencies.
type Scanner struct {
	DB     *commandsdb.DB
	Pool   *pool.Pool
	Events Events

	// VerifyAttempts is how many times a blasted SET is (re)sent within one cycle
	// when the device's echoed value doesn't match what we sent (0 => 3). The
	// background poll loop still reconciles afterwards. VerifyDelay is the pause
	// between attempts (0 => 50ms; tests set it tiny).
	VerifyAttempts int
	VerifyDelay    time.Duration
}

func (s *Scanner) verifyAttempts() int {
	if s.VerifyAttempts > 0 {
		return s.VerifyAttempts
	}
	return 3
}

func (s *Scanner) verifyDelay() time.Duration {
	if s.VerifyDelay != 0 {
		return s.VerifyDelay
	}
	return 50 * time.Millisecond
}

// Command routing tables (main.ts:139-140).
var (
	frameCommandsList = map[uint32]bool{
		4108: true, 4101: true, 4103: true, 4105: true,
		4208: true, 4201: true, 4203: true, 4205: true,
	}
	shufflesList  = buildShufflesList()
	shuffleLabels = []string{"Pass-through", "All Mute", "All Tone", "Custom"}
	ioRe          = regexp.MustCompile(`([0-9]{1,2}) In.*?([0-9]{1,2}) Out`)

	// card status block read per slot (main.ts:542)
	cardStatusCmds = []uint32{4101, 4103, 4105, 4108, 4128, 4129, 4201, 4203, 4205, 4208, 4228, 4229}
)

func buildShufflesList() map[uint32]bool {
	m := make(map[uint32]bool, 16)
	for i := 0; i < 16; i++ {
		m[uint32(50265+300*i)] = true
	}
	return m
}

// CheckFrame scans (and, if enabled, blasts) one frame, mutating `frame`. groups
// is a read-only snapshot; conns supplies device connections by IP.
func (s *Scanner) CheckFrame(ctx context.Context, frame *model.Frame, groups model.Groups, conns Conns) {
	frameIP := frame.IP

	if !frame.Scan {
		frame.Done = true
		s.Events.FrameStatus(frameIP, "Not Scanning", frame.Offline)
		return
	}
	frame.Done = false
	s.Events.FrameStatus(frameIP, "Connecting to frame", frame.Offline)

	address, err := s.getFrameAddress(ctx, conns, frameIP)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		frame.Offline = true
		// Distinguish "can't open the TCP connection" (network/port/firewall)
		// from "connected but no RollCall reply" (handshake/addressing), and log
		// the underlying cause so a remote failure is diagnosable.
		status := "Cannot reach frame"
		if errors.Is(err, device.ErrFrameNoResponse) {
			status = "Frame connected but not responding"
		}
		slog.Warn("frame address discovery failed", "frame", frameIP, "status", status, "err", err)
		s.Events.FrameStatus(frameIP, status, frame.Offline)
		frame.Done = true
		return
	}
	frame.Offline = false

	// Slot discovery: 16530..16549 (main.ts:490-493).
	discCmds := make([]uint32, 0, 20)
	for slot := 0; slot < 20; slot++ {
		discCmds = append(discCmds, uint32(16530+slot))
	}
	s.Events.FrameStatus(frameIP, "Discovering cards within frame", frame.Offline)
	slotsData, _ := s.batchGet(ctx, conns, frameIP, address, "00", discCmds)
	if ctx.Err() != nil {
		return
	}
	s.Events.FrameStatus(frameIP, "Getting cards current config", frame.Offline)

	if frame.Slots == nil {
		frame.Slots = map[string]*model.Slot{}
	}

	foundSlots := make([]string, 0, len(slotsData))
	for cmd, val := range slotsData {
		slot := fmt.Sprintf("%02d", int(cmd)-16529) // decimal slot key
		isUCP := val.Kind == model.KindStr &&
			(strings.Contains(val.Str, "IQUCP25_SDI") || strings.Contains(val.Str, "IQUCP_MADI"))
		if !isUCP {
			if existing := frame.Slots[slot]; existing != nil {
				existing.Offline = true
			}
			continue
		}
		if frame.Slots[slot] == nil {
			frame.Slots[slot] = model.NewSlot()
		}
		frame.Slots[slot].Offline = false
		frame.Slots[slot].Staged = false // a real card is now present: no longer just pre-staged
		foundSlots = append(foundSlots, slot)
	}

	// Per-slot scan + blast, concurrently (main.ts foundSlots.forEach + Promise.all).
	var restart map[uint32]string
	if s.DB != nil {
		restart = s.DB.RestartNames()
	}
	var wg sync.WaitGroup
	for _, slot := range foundSlots {
		wg.Add(1)
		go func(slot string) {
			defer wg.Done()
			s.scanSlot(ctx, frame, slot, address, groups, conns, restart)
		}(slot)
	}
	wg.Wait()

	frame.Done = true
	if ctx.Err() == nil {
		s.Events.FrameStatus(frameIP, "Done", frame.Offline)
	}
}

// Reboot ports the cardReboot handler (main.ts:321-331): resolve the frame unit
// address then SET command 4114 to 1 at that address and the slot's hex token.
func (s *Scanner) Reboot(ctx context.Context, conns Conns, frameIP, slot string) error {
	address, err := s.getFrameAddress(ctx, conns, frameIP)
	if err != nil {
		return err
	}
	dev, err := conns.Device(ctx, frameIP)
	if err != nil {
		return err
	}
	slotHex := fmt.Sprintf("%02x", mustAtoi(slot))
	_, err = dev.Set(ctx, address, slotHex, 4114, model.IntVal(1))
	return err
}

// getFrameAddress ports main.ts:448-467. Returns the hex address token verbatim
// (never int-parsed) or an error meaning the frame is unreachable.
func (s *Scanner) getFrameAddress(ctx context.Context, conns Conns, frameIP string) (string, error) {
	vals, err := s.batchGet(ctx, conns, frameIP, "00", "00", []uint32{17044, 16482})
	if err != nil {
		return "10", err
	}
	if v, ok := vals[17044]; ok && v.String() != "Not In Use" {
		str := v.String()
		if i := strings.Index(str, "= 0x"); i >= 0 {
			addr := strings.TrimSpace(str[i+len("= 0x"):])
			if addr == "" {
				addr = "10"
			}
			return addr, nil
		}
		return "10", nil
	}
	if v, ok := vals[16482]; ok {
		if parts := strings.Split(v.String(), ":"); len(parts) > 1 {
			addr := strings.TrimSpace(parts[1])
			if addr == "" {
				addr = "01"
			}
			return addr, nil
		}
	}
	return "10", fmt.Errorf("failed to get unit address")
}

// scanSlot ports the per-slot body of checkFrame (main.ts:538-742).
func (s *Scanner) scanSlot(ctx context.Context, frame *model.Frame, slot, address string, groups model.Groups, conns Conns, restart map[uint32]string) {
	frameIP := frame.IP
	slotHex := fmt.Sprintf("%02x", mustAtoi(slot))
	checkNull := false

	cardVals, batchErr := s.batchGet(ctx, conns, frameIP, address, slotHex, cardStatusCmds)
	cardAIP := getv(cardVals, 4101)
	cardAMask := getv(cardVals, 4103)
	cardAGate := getv(cardVals, 4105)
	cardAMode := getv(cardVals, 4108)
	cardAUP := getv(cardVals, 4128)
	cardASFP := getv(cardVals, 4129)
	cardBIP := getv(cardVals, 4201)
	cardBMask := getv(cardVals, 4203)
	cardBGate := getv(cardVals, 4205)
	cardBMode := getv(cardVals, 4208)
	cardBUP := getv(cardVals, 4228)
	cardBSFP := getv(cardVals, 4229)

	sl := frame.Slots[slot]
	if sl == nil {
		sl = model.NewSlot()
		frame.Slots[slot] = sl
	}
	sl.IPA = ipPtr(cardAIP)
	sl.IPB = ipPtr(cardBIP)
	sl.IPAUp = cardAUP.String()
	sl.IPBUp = cardBUP.String()
	sl.SFP1 = cardASFP.String()
	sl.SFP2 = cardBSFP.String()

	if batchErr != nil {
		if ctx.Err() != nil {
			return
		}
		s.Events.FrameStatus(frameIP, fmt.Sprintf("Slot: %s error resolving IPs", slot), frame.Offline)
		sl.Offline = true
		return
	}

	// Choose the IP to talk to the card directly (main.ts:580-591).
	requestIP := ""
	if cardAUP.String() == "UP" && !device.IsAbsent(cardAIP) && cardAIP.String() != "No rollcall connection" {
		requestIP = cardAIP.String()
	} else if cardBUP.String() == "UP" && !device.IsAbsent(cardBIP) && cardBIP.String() != "No rollcall connection" {
		requestIP = cardBIP.String()
	}

	if sl.Active == nil {
		sl.Active = map[string]model.Value{}
	}

	if requestIP != "" {
		ins, outs, active, err := s.checkCard(ctx, conns, requestIP)
		if err != nil {
			checkNull = true
		} else {
			sl.Ins = ins
			sl.Outs = outs
			if active != nil {
				sl.Active = active
			}
		}
	}

	// Overlay the card A/B IP/mask/gate/mode values onto active (main.ts:607-632).
	setActive(sl, "4101", cardAIP)
	setActive(sl, "4103", cardAMask)
	setActive(sl, "4105", cardAGate)
	setActive(sl, "4108", cardAMode)
	setActive(sl, "4201", cardBIP)
	setActive(sl, "4203", cardBMask)
	setActive(sl, "4205", cardBGate)
	setActive(sl, "4208", cardBMode)

	sl.Group = computeGroupCommands(frame.Group, frame.Number, slot, frameIP, groups, s.Events)

	if ctx.Err() != nil { // frame deleted/superseded mid-scan: don't redraw or blast
		return
	}

	frameCommands, cardCommands, frameTakes, cardTakes := buildCommands(sl, checkNull)

	if !frame.Enabled || !sl.Enabled {
		// Not blasting: nothing was sent, so clear any reboot-needed/failed flags.
		sl.RebootNeeded = false
		sl.RebootReasons = nil
		sl.Failed = nil
		s.Events.SlotInfo(frameIP, frame, slot, sl)
		s.Events.FrameStatus(frameIP, fmt.Sprintf("Slot: %s not enabled", slot), frame.Offline)
		return
	}
	if ctx.Err() != nil {
		return
	}
	s.Events.FrameStatus(frameIP, fmt.Sprintf("Blasting slot: %s", slot), frame.Offline)

	// A frame command is by definition an IP/mode change, so the card's network
	// identity is about to change: defer the direct bulk push (which would target
	// the about-to-change IP) until a later cycle once the IP has settled.
	failed := map[string]string{}
	deferDirect := len(frameCommands) > 0
	if len(frameCommands) > 0 {
		for k, v := range s.doCommands(ctx, conns, frameCommands, frameTakes, frameIP, address, slotHex) {
			failed[k] = v
		}
	}
	directSent := false
	switch {
	case !deferDirect && (cardAUP.String() == "UP" || cardBUP.String() == "UP"):
		for k, v := range s.doCommands(ctx, conns, cardCommands, cardTakes, requestIP, "30", "00") {
			failed[k] = v
		}
		directSent = true
	case deferDirect && len(cardCommands) > 0:
		s.Events.FrameStatus(frameIP, fmt.Sprintf("Slot: %s IP changing, deferring direct settings until reboot", slot), frame.Offline)
	}
	if len(failed) > 0 {
		sl.Failed = failed
	} else {
		sl.Failed = nil
	}

	// Reboot needed = restart-flagged commands we actually sent this cycle.
	reasons := rebootReasons(frameCommands, sl.Active, restart)
	if directSent {
		reasons = append(reasons, rebootReasons(cardCommands, sl.Active, restart)...)
	}
	sl.RebootNeeded = len(reasons) > 0
	sl.RebootReasons = reasons
	s.Events.SlotInfo(frameIP, frame, slot, sl)
}

// checkCard ports main.ts:869-928 (talks to the card's own IP at addr 30/slot 00).
func (s *Scanner) checkCard(ctx context.Context, conns Conns, cardIP string) (ins, outs int, active map[string]model.Value, err error) {
	active = map[string]model.Value{}
	ioVals, err := s.batchGet(ctx, conns, cardIP, "30", "00", []uint32{18000})
	if err != nil {
		return 0, 0, active, err
	}
	m := ioRe.FindStringSubmatch(ioVals[18000].String())
	if m == nil {
		return 0, 0, active, fmt.Errorf("Unable to match on IO string")
	}
	ins = mustAtoi(m[1])
	outs = mustAtoi(m[2])

	ids := s.DB.CardScanIDs()
	data, derr := s.batchGet(ctx, conns, cardIP, "30", "00", ids)
	if derr != nil {
		return ins, outs, active, derr
	}
	for cmd, v := range data {
		active[strconv.FormatUint(uint64(cmd), 10)] = v
	}
	return ins, outs, active, nil
}

// --- device helpers (the getInfo equivalent) ---

// batchGet reads cmds from one (ip,addr,slot), bounded by the global pool. It
// returns an error only when the whole read failed (nothing came back), matching
// getInfo's "No return data" path; partial failures are silently dropped like
// the legacy parse.
func (s *Scanner) batchGet(ctx context.Context, conns Conns, ip, addr, slot string, cmds []uint32) (map[uint32]model.Value, error) {
	dev, err := conns.Device(ctx, ip)
	if err != nil {
		// TCP connect failed — preserve the underlying cause (refused/timeout/...).
		return nil, fmt.Errorf("connect %s: %w", ip, err)
	}
	if err := s.Pool.Acquire(ctx); err != nil {
		return nil, err
	}
	defer s.Pool.Release()
	vals, errs := dev.BatchGet(ctx, addr, slot, cmds)
	if len(vals) == 0 && len(cmds) > 0 {
		if ctx.Err() != nil {
			return vals, ctx.Err()
		}
		// Connected, but nothing came back: surface a representative cause and tag
		// it so the caller can report "connected but not responding".
		return vals, fmt.Errorf("no reply from %s addr=%s slot=%s (%v): %w", ip, addr, slot, firstErr(errs), device.ErrFrameNoResponse)
	}
	return vals, nil
}

// firstErr returns any one underlying GET error (they are typically the same
// per-call deadline/offline), or a generic fallback.
func firstErr(errs map[uint32]error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return device.ErrFrameNoResponse
}

func getv(m map[uint32]model.Value, cmd uint32) model.Value {
	if v, ok := m[cmd]; ok {
		return v
	}
	return model.None()
}

func ipPtr(v model.Value) *string {
	if v.IsNone() {
		return nil
	}
	s := v.String()
	return &s
}

// setActive mirrors `if (x !== "StringVal" && x !== "No rollcall connection") active[cmd]=x`.
func setActive(sl *model.Slot, key string, v model.Value) {
	if device.IsAbsent(v) {
		return
	}
	if v.Kind == model.KindStr && v.Str == "No rollcall connection" {
		return
	}
	sl.Active[key] = v
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

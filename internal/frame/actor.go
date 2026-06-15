// Package frame implements the per-frame actor: one goroutine owns one frame's
// state and is the only writer of it. Poll ticks AND operator edits arrive as
// messages on a single channel, so a scan can never interleave with an edit or a
// delete — the structural fix for the documented scan-loop race (memory note
// scan-loop-race) that the legacy app papered over with a `done` boolean.
//
// A scan is long (hundreds of round-trips), so it runs in a child goroutine on a
// copy of the frame and posts its result back as a message; the actor merges
// only the scanned fields, preserving any edits made during the scan. A delete
// or reconfigure cancels the in-flight scan's context and bumps a generation
// counter so the stale result is discarded.
package frame

import (
	"context"
	"log/slog"
	"time"

	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/scan"
)

// connIdleTTL is how long an unused per-IP connection is kept before Prune
// closes it (well above the poll interval; covers a card's old IP after a change).
const connIdleTTL = 60 * time.Second

// Callbacks the actor needs from its owner (the manager), passed at construction
// to avoid an import cycle.
type Deps struct {
	Scanner  *scan.Scanner
	Dialer   device.Dialer
	GroupsFn func() model.Groups                // current groups snapshot
	OnChange func(ip string, snap *model.Frame) // push an updated copy upstream
	Save     func()                             // request a (debounced) persist

	// AutoRebootDefault is the global default for auto-rebooting a card after a
	// restart-required change; the per-frame AutoReboot override can force it
	// on/off. Nil => off.
	AutoRebootDefault func() bool
	// AutoRebootCooldown bounds how often one slot may be auto-rebooted (0 => 120s).
	AutoRebootCooldown time.Duration
	// Audit records a destructive auto-action (nil-safe).
	Audit func(action string, detail any)
}

// --- messages ---

type pollTick struct{}
type applyConfig struct{ number, name, group, typ string }
type setCommandMsg struct {
	slot, command, dataType string
	value                   model.Value
	enabled                 bool
	take                    model.Num
}
type setEnableMsg struct {
	slot, command, dataType string
	enabled                 bool
	take                    model.Num
}
type enableFrameMsg struct{ enabled bool }
type scanFrameMsg struct{ scan bool }
type enableSlotMsg struct {
	slot    string
	enabled bool
}
type rebootMsg struct{ slot string }
type stageCardMsg struct{ slot string }
type removeCardMsg struct{ slot string }
type setAutoRebootMsg struct{ mode string } // "" inherit / "on" / "off"
type importFrameMsg struct{ in *model.Frame }
type scanResultMsg struct {
	gen     uint64
	working *model.Frame
}
type snapshotMsg struct{ reply chan *model.Frame }
type stopMsg struct{ reply chan struct{} }

// Actor owns one frame.
type Actor struct {
	ip    string
	frame *model.Frame
	deps  Deps
	conns *connCache

	in   chan any
	done chan struct{}

	inflight   bool
	gen        uint64
	scanCancel context.CancelFunc

	now            func() time.Time
	lastAutoReboot map[string]time.Time
}

// New creates (but does not start) an actor for the given frame copy.
func New(f *model.Frame, deps Deps) *Actor {
	return &Actor{
		ip:             f.IP,
		frame:          cloneFrame(f),
		deps:           deps,
		conns:          newConnCache(deps.Dialer),
		in:             make(chan any, 64),
		done:           make(chan struct{}),
		now:            time.Now,
		lastAutoReboot: map[string]time.Time{},
	}
}

// Start launches the actor goroutine and triggers an immediate scan.
func (a *Actor) Start(ctx context.Context) {
	go a.run(ctx)
	a.Poll()
}

// --- public message senders (called by the manager) ---

func (a *Actor) Poll() { a.send(pollTick{}) }
func (a *Actor) ApplyConfig(number, name, group, typ string) {
	a.send(applyConfig{number, name, group, typ})
}
func (a *Actor) Reboot(slot string)              { a.send(rebootMsg{slot}) }
func (a *Actor) StageCard(slot string)           { a.send(stageCardMsg{slot}) }
func (a *Actor) RemoveCard(slot string)          { a.send(removeCardMsg{slot}) }
func (a *Actor) SetAutoReboot(mode string)       { a.send(setAutoRebootMsg{mode}) }
func (a *Actor) ImportFrame(in *model.Frame)     { a.send(importFrameMsg{in}) }
func (a *Actor) EnableFrame(enabled bool)        { a.send(enableFrameMsg{enabled}) }
func (a *Actor) ScanFrame(scan bool)             { a.send(scanFrameMsg{scan}) }
func (a *Actor) EnableSlot(slot string, en bool) { a.send(enableSlotMsg{slot, en}) }

func (a *Actor) SetCommand(slot, command string, value model.Value, enabled bool, dataType string, take model.Num) {
	a.send(setCommandMsg{slot: slot, command: command, dataType: dataType, value: value, enabled: enabled, take: take})
}
func (a *Actor) SetEnable(slot, command string, enabled bool, dataType string, take model.Num) {
	a.send(setEnableMsg{slot: slot, command: command, dataType: dataType, enabled: enabled, take: take})
}

// Snapshot returns a copy of the current frame state (synchronous).
func (a *Actor) Snapshot() *model.Frame {
	reply := make(chan *model.Frame, 1)
	select {
	case a.in <- snapshotMsg{reply}:
		return <-reply
	case <-a.done:
		return nil
	}
}

// Stop shuts the actor down (cancelling any in-flight scan) and waits.
func (a *Actor) Stop() {
	reply := make(chan struct{})
	select {
	case a.in <- stopMsg{reply}:
		<-reply
	case <-a.done:
	}
}

func (a *Actor) send(m any) {
	select {
	case a.in <- m:
	case <-a.done:
	}
}

// --- run loop ---

func (a *Actor) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			a.cleanup()
			return
		case m := <-a.in:
			switch v := m.(type) {
			case pollTick:
				a.handlePoll(ctx)
			case scanResultMsg:
				a.handleScanResult(ctx, v)
			case applyConfig:
				a.cancelScan() // settings changed -> abandon the in-flight scan
				a.frame.Number, a.frame.Name, a.frame.Group, a.frame.Type = v.number, v.name, v.group, v.typ
				a.changed()
			case setCommandMsg:
				a.setCommand(v)
				a.changed()
			case setEnableMsg:
				a.setEnable(v)
				a.changed()
			case enableFrameMsg:
				a.frame.Enabled = v.enabled
				a.changed()
			case scanFrameMsg:
				a.frame.Scan = v.scan
				a.frame.Done = true
				a.changed()
			case setAutoRebootMsg:
				a.frame.AutoReboot = v.mode
				a.changed()
			case importFrameMsg:
				a.importFrame(v.in)
				a.changed()
			case enableSlotMsg:
				if sl := a.frame.Slots[v.slot]; sl != nil {
					sl.Enabled = v.enabled
					a.changed()
				}
			case rebootMsg:
				go func(slot string) { _ = a.deps.Scanner.Reboot(ctx, a.conns, a.ip, slot) }(v.slot)
			case stageCardMsg:
				sl := a.ensureSlot(v.slot)
				sl.Staged = true
				a.changed()
			case removeCardMsg:
				if _, ok := a.frame.Slots[v.slot]; ok {
					delete(a.frame.Slots, v.slot)
					a.changed()
				}
			case snapshotMsg:
				v.reply <- cloneFrame(a.frame)
			case stopMsg:
				a.cleanup()
				close(v.reply)
				return
			}
		}
	}
}

func (a *Actor) handlePoll(ctx context.Context) {
	if a.inflight {
		return // a scan is already running; drop the tick (replaces the `done` gate)
	}
	a.inflight = true
	a.gen++
	gen := a.gen
	scanCtx, cancel := context.WithCancel(ctx)
	a.scanCancel = cancel
	working := cloneFrame(a.frame)
	groups := a.deps.GroupsFn()
	go func() {
		a.deps.Scanner.CheckFrame(scanCtx, working, groups, a.conns)
		select {
		case a.in <- scanResultMsg{gen: gen, working: working}:
		case <-a.done:
		}
	}()
}

func (a *Actor) handleScanResult(ctx context.Context, v scanResultMsg) {
	if v.gen != a.gen {
		return // superseded by a cancel/reconfigure -> discard
	}
	a.inflight = false
	if a.scanCancel != nil {
		a.scanCancel()
		a.scanCancel = nil
	}
	mergeScanned(a.frame, v.working)
	a.maybeAutoReboot(ctx)
	a.conns.Prune(connIdleTTL) // drop the old IP after a card's IP changed
	a.changed()
}

// effectiveAutoReboot resolves the per-frame override against the global default.
func (a *Actor) effectiveAutoReboot() bool {
	switch a.frame.AutoReboot {
	case "on":
		return true
	case "off":
		return false
	default: // inherit
		return a.deps.AutoRebootDefault != nil && a.deps.AutoRebootDefault()
	}
}

// maybeAutoReboot reboots any slot that needs it, when auto-reboot is effective
// for this frame, subject to a per-slot cooldown (a reboot takes ~1-2 min).
func (a *Actor) maybeAutoReboot(ctx context.Context) {
	if !a.effectiveAutoReboot() {
		return
	}
	cooldown := a.deps.AutoRebootCooldown
	if cooldown <= 0 {
		cooldown = 120 * time.Second
	}
	now := a.now()
	for slot, sl := range a.frame.Slots {
		if sl == nil || !sl.RebootNeeded {
			continue
		}
		if last, ok := a.lastAutoReboot[slot]; ok && now.Sub(last) < cooldown {
			continue // within cooldown — a reboot is already in progress
		}
		a.lastAutoReboot[slot] = now
		reasons := sl.RebootReasons
		slog.Warn("auto-reboot", "frame", a.ip, "slot", slot, "reasons", reasons)
		if a.deps.Audit != nil {
			a.deps.Audit("autoReboot", map[string]any{"frameIP": a.ip, "slot": slot, "reasons": reasons})
		}
		go func(slot string) { _ = a.deps.Scanner.Reboot(ctx, a.conns, a.ip, slot) }(slot)
	}
}

func (a *Actor) cancelScan() {
	if a.scanCancel != nil {
		a.scanCancel()
		a.scanCancel = nil
	}
	a.inflight = false
	a.gen++ // any in-flight result now carries a stale gen and will be discarded
}

func (a *Actor) cleanup() {
	a.cancelScan()
	close(a.done)
	a.conns.closeAll()
}

func (a *Actor) changed() {
	a.deps.OnChange(a.ip, cloneFrame(a.frame))
	a.deps.Save()
}

// setCommand ports the setCommand handler (main.ts:266-288).
func (a *Actor) setCommand(v setCommandMsg) {
	sl := a.ensureSlot(v.slot)
	if sl.Prefered == nil {
		sl.Prefered = map[string]model.FramePrefered{}
	}
	sl.Prefered[v.command] = model.FramePrefered{
		Value:    v.value,
		Enabled:  v.enabled,
		Type:     v.dataType,
		DataType: v.dataType,
		Take:     v.take,
	}
}

// setEnable ports the setEnable handler (main.ts:289-303).
func (a *Actor) setEnable(v setEnableMsg) {
	sl := a.ensureSlot(v.slot)
	if sl.Prefered == nil {
		sl.Prefered = map[string]model.FramePrefered{}
	}
	p, ok := sl.Prefered[v.command]
	if ok {
		p.Enabled = v.enabled
		sl.Prefered[v.command] = p
	} else {
		sl.Prefered[v.command] = model.FramePrefered{Value: model.None(), Enabled: v.enabled, Type: "text"}
	}
}

// importFrame merges an imported frame's CONFIGURATION into this actor's frame.
// It restores metadata, group assignment, auto-reboot, scanning, and per-slot
// prefered overrides + staged/enabled, but deliberately never turns ON frame
// blasting (a.frame.Enabled is left as-is) — importing config must not start
// blasting live hardware. Runtime fields (active/IPs/offline) are left to the scan.
func (a *Actor) importFrame(in *model.Frame) {
	if in.Number != "" {
		a.frame.Number = in.Number
	}
	a.frame.Name = in.Name
	a.frame.Group = in.Group
	if in.Type != "" {
		a.frame.Type = in.Type
	}
	a.frame.Scan = in.Scan
	a.frame.AutoReboot = in.AutoReboot
	if a.frame.Slots == nil {
		a.frame.Slots = map[string]*model.Slot{}
	}
	for name, isl := range in.Slots {
		if isl == nil {
			continue
		}
		sl := a.frame.Slots[name]
		if sl == nil {
			sl = model.NewSlot()
			a.frame.Slots[name] = sl
		}
		if isl.Prefered != nil {
			sl.Prefered = isl.Prefered
		}
		sl.Enabled = isl.Enabled
		sl.Staged = isl.Staged
	}
}

func (a *Actor) ensureSlot(slot string) *model.Slot {
	if a.frame.Slots == nil {
		a.frame.Slots = map[string]*model.Slot{}
	}
	sl := a.frame.Slots[slot]
	if sl == nil {
		sl = model.NewSlot()
		a.frame.Slots[slot] = sl
	}
	return sl
}

// --- clone & merge ---

func cloneFrame(f *model.Frame) *model.Frame { return model.CloneFrame(f) }

// mergeScanned copies only the device-scanned fields from src into dst, leaving
// operator-editable fields (prefered, slot.enabled, frame.enabled/scan/group/...)
// untouched so an edit made during the scan is not lost.
func mergeScanned(dst, src *model.Frame) {
	dst.Offline = src.Offline
	dst.Done = src.Done
	if dst.Slots == nil {
		dst.Slots = map[string]*model.Slot{}
	}
	for name, ss := range src.Slots {
		ds := dst.Slots[name]
		if ds == nil {
			ds = model.NewSlot()
			ds.Enabled = ss.Enabled
			dst.Slots[name] = ds
		}
		ds.Offline = ss.Offline
		ds.Staged = ss.Staged // discovery clears Staged once a real card is present
		ds.IPA, ds.IPB = ss.IPA, ss.IPB
		ds.IPAUp, ds.IPBUp = ss.IPAUp, ss.IPBUp
		ds.SFP1, ds.SFP2 = ss.SFP1, ss.SFP2
		ds.Ins, ds.Outs = ss.Ins, ss.Outs
		ds.Active = ss.Active
		ds.Group = ss.Group
		ds.RebootNeeded = ss.RebootNeeded
		ds.RebootReasons = ss.RebootReasons
		ds.Failed = ss.Failed
	}
}

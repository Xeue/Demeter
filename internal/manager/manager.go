// Package manager owns the set of per-frame actors, the groups map, the poll
// ticker, and the persistence/broadcast seams. It is the entry point the web/hub
// tier calls for every inbound UI command; it routes edits to the relevant
// actor and serves frame/group snapshots back to clients.
package manager

import (
	"context"
	"sync"
	"time"

	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/frame"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/scan"
)

// Persister persists frames/groups. now=true forces an immediate write (used for
// deletes and bulk imports); otherwise the store coalesces.
type Persister interface {
	SaveFrames(frames model.Frames, now bool)
	SaveGroups(groups model.Groups, now bool)
}

// Broadcaster pushes full frame/group maps to all connected clients.
type Broadcaster interface {
	BroadcastFrames(model.Frames)
	BroadcastGroups(model.Groups)
}

// AutoRebootOptions configures the auto-reboot feature (see actor.maybeAutoReboot).
type AutoRebootOptions struct {
	Default  bool                            // global default (per-frame override wins)
	Cooldown time.Duration                   // min time between auto-reboots of one slot
	Audit    func(action string, detail any) // records an auto-reboot (nil-safe)
	Persist  func(enabled bool)              // persists a changed global default (nil-safe)
}

// Manager coordinates actors, groups, persistence and broadcasts.
type Manager struct {
	ctx          context.Context
	scanner      *scan.Scanner
	dialer       device.Dialer
	store        Persister
	bcast        Broadcaster
	pollInterval time.Duration

	auto AutoRebootOptions

	mu                sync.Mutex
	actors            map[string]*frame.Actor
	framesView        map[string]*model.Frame
	groups            model.Groups
	autoRebootDefault bool
}

// New builds a manager from loaded frames/groups (actors are created but not
// started until Start).
func New(ctx context.Context, scanner *scan.Scanner, dialer device.Dialer, store Persister, bcast Broadcaster, frames model.Frames, groups model.Groups, pollInterval time.Duration, auto AutoRebootOptions) *Manager {
	if pollInterval <= 0 {
		pollInterval = 3 * time.Second
	}
	if groups == nil {
		groups = model.Groups{}
	}
	m := &Manager{
		ctx: ctx, scanner: scanner, dialer: dialer, store: store, bcast: bcast,
		pollInterval:      pollInterval,
		auto:              auto,
		actors:            map[string]*frame.Actor{},
		framesView:        map[string]*model.Frame{},
		groups:            groups,
		autoRebootDefault: auto.Default,
	}
	for ip, f := range frames {
		m.framesView[ip] = model.CloneFrame(f)
		m.actors[ip] = frame.New(f, m.deps())
	}
	return m
}

// Start launches all actors and the poll ticker.
func (m *Manager) Start() {
	m.mu.Lock()
	for _, a := range m.actors {
		a.Start(m.ctx)
	}
	m.mu.Unlock()
	go m.pollLoop()
}

func (m *Manager) pollLoop() {
	t := time.NewTicker(m.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			actors := make([]*frame.Actor, 0, len(m.actors))
			for _, a := range m.actors {
				actors = append(actors, a)
			}
			m.mu.Unlock()
			for _, a := range actors {
				a.Poll()
			}
		}
	}
}

func (m *Manager) deps() frame.Deps {
	return frame.Deps{
		Scanner:            m.scanner,
		Dialer:             m.dialer,
		GroupsFn:           m.GroupsSnapshot,
		OnChange:           m.onFrameChange,
		Save:               m.saveFrames,
		AutoRebootDefault:  m.GlobalAutoReboot, // read live so a runtime toggle takes effect
		AutoRebootCooldown: m.auto.Cooldown,
		Audit:              m.auto.Audit,
	}
}

// GlobalAutoReboot returns the current global auto-reboot default.
func (m *Manager) GlobalAutoReboot() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.autoRebootDefault
}

func (m *Manager) onFrameChange(ip string, snap *model.Frame) {
	m.mu.Lock()
	m.framesView[ip] = snap
	m.mu.Unlock()
}

func (m *Manager) saveFrames() {
	m.store.SaveFrames(m.FramesSnapshot(), false)
}

// --- snapshots for clients/persistence ---

// FramesSnapshot returns a deep copy of the current frames.
func (m *Manager) FramesSnapshot() model.Frames {
	m.mu.Lock()
	defer m.mu.Unlock()
	return model.CloneFrames(m.framesView)
}

// GroupsSnapshot returns a deep copy of the current groups.
func (m *Manager) GroupsSnapshot() model.Groups {
	m.mu.Lock()
	defer m.mu.Unlock()
	return model.CloneGroups(m.groups)
}

func (m *Manager) actor(ip string) *frame.Actor {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.actors[ip]
}

// --- inbound frame commands (routed to actors) ---

// AddFrame upserts a frame (main.ts addFrame): updates an existing one's
// number/name/group/type, or creates a new one (enabled=false, scan=true).
func (m *Manager) AddFrame(ip, number, name, group, typ string) {
	m.mu.Lock()
	a, ok := m.actors[ip]
	if ok {
		m.mu.Unlock()
		a.ApplyConfig(number, name, group, typ)
	} else {
		f := &model.Frame{
			Number: number, Name: name, IP: ip,
			Enabled: false, Scan: true, Group: group, Type: typ,
			Done: true, Slots: map[string]*model.Slot{},
		}
		m.framesView[ip] = model.CloneFrame(f)
		na := frame.New(f, m.deps())
		m.actors[ip] = na
		m.mu.Unlock()
		na.Start(m.ctx)
	}
	m.bcast.BroadcastFrames(m.FramesSnapshot())
	m.store.SaveFrames(m.FramesSnapshot(), false)
}

// DeleteFrame removes a frame and persists immediately (main.ts deleteFrame).
func (m *Manager) DeleteFrame(ip string) {
	m.mu.Lock()
	a := m.actors[ip]
	delete(m.actors, ip)
	delete(m.framesView, ip)
	snap := model.CloneFrames(m.framesView)
	m.mu.Unlock()
	if a != nil {
		a.Stop()
	}
	m.bcast.BroadcastFrames(snap)
	m.store.SaveFrames(snap, true)
}

// SetCommand sets a per-card prefered override.
func (m *Manager) SetCommand(ip, slot, command string, value model.Value, enabled bool, dataType string, take model.Num) {
	if a := m.actor(ip); a != nil {
		a.SetCommand(slot, command, value, enabled, dataType, take)
	}
}

// SetEnable toggles a prefered override's enabled flag.
func (m *Manager) SetEnable(ip, slot, command string, enabled bool, dataType string, take model.Num) {
	if a := m.actor(ip); a != nil {
		a.SetEnable(slot, command, enabled, dataType, take)
	}
}

// EnableFrame toggles whether a frame is blasted.
func (m *Manager) EnableFrame(ip string, enabled bool) {
	if a := m.actor(ip); a != nil {
		a.EnableFrame(enabled)
	}
}

// ScanFrame toggles scanning.
func (m *Manager) ScanFrame(ip string, scanOn bool) {
	if a := m.actor(ip); a != nil {
		a.ScanFrame(scanOn)
	}
}

// EnableSlot toggles whether a slot is blasted.
func (m *Manager) EnableSlot(ip, slot string, enabled bool) {
	if a := m.actor(ip); a != nil {
		a.EnableSlot(slot, enabled)
	}
}

// Reboot reboots a card (cardReboot).
func (m *Manager) Reboot(ip, slot string) {
	if a := m.actor(ip); a != nil {
		a.Reboot(slot)
	}
}

// SetAutoReboot sets a frame's per-frame auto-reboot override ("", "on", "off").
func (m *Manager) SetAutoReboot(ip, mode string) {
	switch mode {
	case "", "on", "off":
	default:
		return
	}
	if a := m.actor(ip); a != nil {
		a.SetAutoReboot(mode)
	}
}

// SetGlobalAutoReboot updates the global auto-reboot default, persists it, and
// rebroadcasts frames so the UI reflects the new effective state.
func (m *Manager) SetGlobalAutoReboot(enabled bool) {
	m.mu.Lock()
	m.autoRebootDefault = enabled
	m.mu.Unlock()
	if m.auto.Persist != nil {
		m.auto.Persist(enabled)
	}
	m.bcast.BroadcastFrames(m.FramesSnapshot())
}

// StageCard pre-creates a slot so its per-card overrides can be configured before
// the card is online; the scan applies them when the card is discovered.
func (m *Manager) StageCard(ip, slot string) {
	if a := m.actor(ip); a != nil {
		a.StageCard(slot)
	}
}

// RemoveCard drops a slot (used to unstage an expected card).
func (m *Manager) RemoveCard(ip, slot string) {
	if a := m.actor(ip); a != nil {
		a.RemoveCard(slot)
	}
}

// SetFrames replaces the entire frames map (bulk import); persists immediately.
func (m *Manager) SetFrames(frames model.Frames) {
	m.mu.Lock()
	old := m.actors
	m.actors = map[string]*frame.Actor{}
	m.framesView = map[string]*model.Frame{}
	for ip, f := range frames {
		f.IP = ip
		m.framesView[ip] = model.CloneFrame(f)
		na := frame.New(f, m.deps())
		m.actors[ip] = na
	}
	newActors := m.actors
	snap := model.CloneFrames(m.framesView)
	m.mu.Unlock()

	for _, a := range old {
		a.Stop()
	}
	for _, a := range newActors {
		a.Start(m.ctx)
	}
	m.bcast.BroadcastFrames(snap)
	m.store.SaveFrames(snap, true)
}

// --- inbound group commands ---

// AddGroup upserts a group (main.ts addGroup: new groups start disabled).
func (m *Manager) AddGroup(name string, enabled bool) {
	m.mu.Lock()
	if g, ok := m.groups[name]; ok {
		g.Name = name
		g.Enabled = enabled
	} else {
		m.groups[name] = &model.Group{Name: name, Enabled: false, Commands: map[string]model.CommandDef{}}
	}
	snap := model.CloneGroups(m.groups)
	m.mu.Unlock()
	m.bcast.BroadcastGroups(snap)
	m.store.SaveGroups(snap, false)
}

// SetGroupCommand sets one command in a group.
func (m *Manager) SetGroupCommand(group, typ, dataType, increment, command string, value model.Value, enabled bool, take model.Num) {
	m.mu.Lock()
	g := m.groups[group]
	if g == nil {
		m.mu.Unlock()
		return
	}
	if g.Commands == nil {
		g.Commands = map[string]model.CommandDef{}
	}
	g.Commands[command] = model.CommandDef{
		Value: value, Enabled: enabled, Type: typ, DataType: dataType,
		Increment: numFromString(increment), Take: take,
	}
	m.mu.Unlock()
	m.store.SaveGroups(m.GroupsSnapshot(), false)
}

// EnableGroup toggles a group's enabled flag.
func (m *Manager) EnableGroup(name string, enabled bool) {
	m.mu.Lock()
	if g := m.groups[name]; g != nil {
		g.Enabled = enabled
	}
	m.mu.Unlock()
	m.store.SaveGroups(m.GroupsSnapshot(), false)
}

// DeleteGroup removes a group.
func (m *Manager) DeleteGroup(name string) {
	m.mu.Lock()
	delete(m.groups, name)
	m.mu.Unlock()
	m.store.SaveGroups(m.GroupsSnapshot(), false)
}

// ExportSnapshot returns a clean, config-only copy of the current frames and
// groups for export: runtime fields (active values, discovered IPs, offline /
// reboot-needed flags, scan-derived group commands) are cleared so the export is
// portable and re-importable. Per-card prefered overrides, staged flags, group
// assignments and metadata are kept.
func (m *Manager) ExportSnapshot() (model.Frames, model.Groups) {
	frames := m.FramesSnapshot()
	for _, f := range frames {
		f.Offline = false
		f.Done = true
		for _, sl := range f.Slots {
			sl.Active = map[string]model.Value{}
			sl.Group = map[string]model.FrameGroup{}
			sl.Offline = false
			sl.RebootNeeded = false
			sl.RebootReasons = nil
			sl.IPA, sl.IPB = nil, nil
			sl.IPAUp, sl.IPBUp, sl.SFP1, sl.SFP2 = "", "", "", ""
			sl.Ins, sl.Outs = 0, 0
			if sl.Prefered == nil {
				sl.Prefered = map[string]model.FramePrefered{}
			}
		}
	}
	return frames, m.GroupsSnapshot()
}

// ImportData merges (upserts) the selected frames and groups into the current
// state without touching anything not present in the import. Existing frames get
// their config merged (see actor.importFrame — blasting is never auto-enabled);
// new frames are created (blasting off). Groups are upserted. Persists + broadcasts.
func (m *Manager) ImportData(frames model.Frames, groups model.Groups) {
	// Groups: upsert.
	m.mu.Lock()
	for name, g := range groups {
		if g == nil {
			continue
		}
		g.Name = name
		m.groups[name] = model.CloneGroup(g)
	}
	type pending struct {
		a *frame.Actor
		f *model.Frame
	}
	var existing []pending
	var toStart []*frame.Actor
	for ip, f := range frames {
		if f == nil {
			continue
		}
		f.IP = ip
		if a, ok := m.actors[ip]; ok {
			existing = append(existing, pending{a, model.CloneFrame(f)})
			continue
		}
		nf := frameFromImport(ip, f)
		m.framesView[ip] = model.CloneFrame(nf)
		na := frame.New(nf, m.deps())
		m.actors[ip] = na
		toStart = append(toStart, na)
	}
	gsnap := model.CloneGroups(m.groups)
	m.mu.Unlock()

	// Merge into existing actors, then read each back (FIFO Snapshot reflects the
	// import) so framesView/broadcast/persist see the merged result.
	for _, p := range existing {
		p.a.ImportFrame(p.f)
		if snap := p.a.Snapshot(); snap != nil {
			m.mu.Lock()
			m.framesView[snap.IP] = snap
			m.mu.Unlock()
		}
	}
	for _, a := range toStart {
		a.Start(m.ctx)
	}

	fsnap := m.FramesSnapshot()
	m.bcast.BroadcastGroups(gsnap)
	m.bcast.BroadcastFrames(fsnap)
	m.store.SaveGroups(gsnap, true)
	m.store.SaveFrames(fsnap, true)
}

// frameFromImport builds a new frame from imported config: blasting off (never
// auto-enable on import), runtime fields cleared, prefered/staged kept.
func frameFromImport(ip string, f *model.Frame) *model.Frame {
	nf := model.CloneFrame(f)
	nf.IP = ip
	nf.Enabled = false
	nf.Offline = false
	nf.Done = true
	if nf.Slots == nil {
		nf.Slots = map[string]*model.Slot{}
	}
	for _, sl := range nf.Slots {
		sl.Active = map[string]model.Value{}
		sl.Group = map[string]model.FrameGroup{}
		sl.Offline = false
		sl.RebootNeeded = false
		sl.RebootReasons = nil
		if sl.Prefered == nil {
			sl.Prefered = map[string]model.FramePrefered{}
		}
	}
	return nf
}

// SetGroups replaces the entire groups map (bulk import); persists immediately
// and also persists frames (main.ts setGroups wrote both).
func (m *Manager) SetGroups(groups model.Groups) {
	m.mu.Lock()
	if groups == nil {
		groups = model.Groups{}
	}
	m.groups = groups
	gsnap := model.CloneGroups(m.groups)
	fsnap := model.CloneFrames(m.framesView)
	m.mu.Unlock()
	m.bcast.BroadcastGroups(gsnap)
	m.store.SaveGroups(gsnap, true)
	m.store.SaveFrames(fsnap, true)
}

// numFromString parses a numeric string (the data-increment attribute) leniently.
func numFromString(s string) model.Num {
	var n model.Num
	_ = n.UnmarshalJSON([]byte(`"` + s + `"`))
	return n
}

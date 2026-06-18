package hub

import (
	"hash/fnv"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/logging"
	"github.com/Xeue/Demeter/internal/model"
)

// Engine is the manager surface the router calls. The manager implements it.
type Engine interface {
	FramesSnapshot() model.Frames
	GroupsSnapshot() model.Groups
	AddFrame(ip, number, name, group, typ string)
	DeleteFrame(ip string)
	SetCommand(ip, slot, command string, value model.Value, enabled bool, dataType string, take model.Num)
	SetEnable(ip, slot, command string, enabled bool, dataType string, take model.Num)
	EnableFrame(ip string, enabled bool)
	ScanFrame(ip string, scanOn bool)
	EnableSlot(ip, slot string, enabled bool)
	Reboot(ip, slot string)
	PollNow(ip string)
	ApplyNow(ip string)
	ScanIntervalSeconds() int
	SetScanInterval(seconds int)
	SetAutoReboot(ip, mode string)
	SetGlobalAutoReboot(enabled bool)
	StageCard(ip, slot string)
	RemoveCard(ip, slot string)
	AddGroup(name string, enabled bool)
	SetGroupCommand(group, typ, dataType, increment, command string, value model.Value, enabled bool, take model.Num)
	EnableGroup(name string, enabled bool)
	DeleteGroup(name string)
	SetGroups(groups model.Groups)
	SetFrames(frames model.Frames)
	GlobalAutoReboot() bool
	ExportSnapshot() (model.Frames, model.Groups)
	ImportData(frames model.Frames, groups model.Groups)
}

// clientSendBuffer is the per-client outbound queue depth (messages). It must
// comfortably exceed the largest realistic burst (a full-fleet discovery), so a
// momentarily-busy browser isn't dropped by fan() before its writePump drains.
// Per-frame coalescing (SlotInfoBatch) keeps a burst to ~one message per frame,
// so this headroom covers a large fleet many times over.
const clientSendBuffer = 1024

// Hub fans outbound messages to all connected clients and dispatches inbound
// messages to the engine. It implements scan.Events, manager.Broadcaster and a
// logging emitter.
type Hub struct {
	engine Engine
	auth   *auth.Auth
	router *Router

	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte

	mu      sync.RWMutex
	clients map[*Client]struct{}

	logMu  sync.Mutex
	logBuf []logging.Event // recent log events, replayed to a client on connect

	// dedup suppresses the per-cycle firehose of UNCHANGED slot/status updates:
	// the scan re-reads and re-emits every slot every cycle, but most cycles
	// change nothing, so re-broadcasting ~20KB per slot would swamp a browser's
	// write queue and get it dropped (losing the later frames' updates). We hash
	// the last bytes sent per key and skip an identical re-send. A (re)connecting
	// client is unaffected: it gets full state from the reliable frames snapshot.
	dedupMu    sync.Mutex
	lastSlot   map[string]uint64 // frameIP|slot -> hash of last slotInfo bytes
	lastStatus map[string]uint64 // frameIP      -> hash of last frameStatus bytes
}

// maxLogBuffer bounds the in-memory log history replayed to new clients.
const maxLogBuffer = 500

// New creates a hub. SetEngine must be called before clients connect.
func New(a *auth.Auth) *Hub {
	h := &Hub{
		auth:       a,
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 4096),
		clients:    map[*Client]struct{}{},
		lastSlot:   map[string]uint64{},
		lastStatus: map[string]uint64{},
	}
	h.router = newRouter(h)
	return h
}

// SetEngine wires the engine (manager) into the hub/router.
func (h *Hub) SetEngine(e Engine) { h.engine = e }

// GlobalAutoReboot reports the current global auto-reboot default (for the page
// bootstrap). False if no engine is wired.
func (h *Hub) GlobalAutoReboot() bool {
	if h.engine == nil {
		return false
	}
	return h.engine.GlobalAutoReboot()
}

// ScanIntervalSeconds reports the current global scan interval (for the page
// bootstrap). 3 if no engine is wired.
func (h *Hub) ScanIntervalSeconds() int {
	if h.engine == nil {
		return 3
	}
	return h.engine.ScanIntervalSeconds()
}

// Run is the hub's event loop.
func (h *Hub) Run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.fan(msg)
		}
	}
}

// fan delivers msg to every client, dropping any whose queue is full (a slow
// client must never back-pressure the scan loop).
func (h *Hub) fan(msg []byte) {
	var slow []*Client
	h.mu.RLock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range slow {
		slog.Warn("hub: dropping slow client", "user", c.username())
		c.cancel()
	}
}

// emitReliable blocks (up to the buffer) so important state messages are not lost.
func (h *Hub) emitReliable(command string, data any) {
	h.broadcast <- encode(command, data)
}

// emitLossy drops the message if the buffer is full (high-frequency status/log).
func (h *Hub) emitLossy(command string, data any) {
	h.emitBytesLossy(encode(command, data))
}

// emitBytesLossy queues pre-encoded bytes, dropping them if the buffer is full.
func (h *Hub) emitBytesLossy(b []byte) {
	select {
	case h.broadcast <- b:
	default:
	}
}

// changedSince reports whether the hashContent for key differs from the last
// seen value, updating it. This is the dedup primitive that collapses the
// per-cycle re-broadcast of unchanged data so a busy client is never swamped
// (and dropped) by redundant updates.
func (h *Hub) changedSince(seen map[string]uint64, key string, hashContent []byte) bool {
	hh := fnv.New64a()
	hh.Write(hashContent)
	sum := hh.Sum64()
	h.dedupMu.Lock()
	defer h.dedupMu.Unlock()
	if seen[key] == sum {
		return false // identical to what we last sent, skip
	}
	seen[key] = sum
	return true
}

// dedupLossy emits payload only when its bytes differ from the last sent for key.
func (h *Hub) dedupLossy(seen map[string]uint64, key string, payload []byte) {
	if h.changedSince(seen, key, payload) {
		h.emitBytesLossy(payload)
	}
}

// --- manager.Broadcaster ---

// BroadcastFrames sends the full frames map to all clients. It also prunes the
// per-slot/per-status dedup caches of any frame no longer present, so deleting
// frames can't leak dedup entries over a long uptime.
func (h *Hub) BroadcastFrames(f model.Frames) {
	h.pruneDedup(f)
	h.emitReliable(chFrames, f)
}

// pruneDedup drops dedup-cache entries for frames absent from f.
func (h *Hub) pruneDedup(f model.Frames) {
	h.dedupMu.Lock()
	defer h.dedupMu.Unlock()
	for ip := range h.lastStatus {
		if _, ok := f[ip]; !ok {
			delete(h.lastStatus, ip)
		}
	}
	for key := range h.lastSlot {
		ip, _, _ := strings.Cut(key, "|")
		if _, ok := f[ip]; !ok {
			delete(h.lastSlot, key)
		}
	}
}

// BroadcastGroups sends the full groups map to all clients.
func (h *Hub) BroadcastGroups(g model.Groups) { h.emitReliable(chGroups, g) }

// --- scan.Events ---

// FrameStatus reports scan progress. Deduped per frame so the repeated
// steady-state "Done"/offline status each cycle isn't re-broadcast.
func (h *Hub) FrameStatus(frameIP, status string, offline bool) {
	payload := encode(chFrameStatus, map[string]any{"frameIP": frameIP, "status": status, "offline": offline})
	h.dedupLossy(h.lastStatus, frameIP, payload)
}

// SlotInfo sends a single per-slot delta, deduped per (frame,slot). Live scans
// use SlotInfoBatch instead; this remains for any one-off per-slot push.
func (h *Hub) SlotInfo(frameIP string, frame *model.Frame, slotName string, slot *model.Slot) {
	msg := slotInfoMsgFor(frame, slotName, slot)
	if h.changedSince(h.lastSlot, frameIP+"|"+slotName, marshalData(msg)) {
		h.emitBytesLossy(encode(chSlotInfo, msg))
	}
}

// SlotInfoBatch coalesces all of a frame's just-scanned slots into ONE message,
// emitted when the frame finishes scanning. Each slot is deduped individually
// (unchanged slots add nothing), so a discovery/rescan burst that used to be N
// per-slot messages becomes one message per frame, keeping a busy client's
// send queue well under its cap instead of overflowing and getting dropped. The
// client fans the items back through its normal per-slot render queue.
func (h *Hub) SlotInfoBatch(frameIP string, frame *model.Frame, slotNames []string) {
	items := make([]slotInfoItem, 0, len(slotNames))
	for _, name := range slotNames {
		slot := frame.Slots[name]
		if slot == nil {
			continue
		}
		msg := slotInfoMsgFor(frame, name, slot)
		if !h.changedSince(h.lastSlot, frameIP+"|"+name, marshalData(msg)) {
			continue // unchanged since last sent
		}
		items = append(items, slotInfoItem{SlotName: name, Slot: msg.Slot})
	}
	if len(items) == 0 {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].SlotName < items[j].SlotName })
	h.emitBytesLossy(encode(chSlotInfoBatch, slotInfoBatchMsg{Frame: frameHeaderOf(frame), Slots: items}))
}

// frameHeaderOf builds the trimmed identity header sent with slot deltas.
func frameHeaderOf(f *model.Frame) frameHeader {
	return frameHeader{
		IP: f.IP, Number: f.Number, Name: f.Name, Group: f.Group,
		Enabled: f.Enabled, Scan: f.Scan, Offline: f.Offline,
	}
}

// slotInfoMsgFor builds a per-slot delta with a deep-cloned slot, so marshaling
// is race-free against the actor that still owns the live slot.
func slotInfoMsgFor(frame *model.Frame, slotName string, slot *model.Slot) slotInfoMsg {
	return slotInfoMsg{
		Frame:    frameHeaderOf(frame),
		SlotName: slotName,
		Slot:     model.CloneFrame(&model.Frame{Slots: map[string]*model.Slot{slotName: slot}}).Slots[slotName],
	}
}

// FrameError reports a per-frame error (note: field is "error", which app.js reads).
func (h *Hub) FrameError(frameIP, errMsg string) {
	h.emitReliable(chFrameError, map[string]any{"frameIP": frameIP, "error": errMsg})
}

// --- logging emitter ---

// Log fans a structured log event to clients (lossy).
func (h *Hub) Log(e logging.Event) {
	h.logMu.Lock()
	h.logBuf = append(h.logBuf, e)
	if len(h.logBuf) > maxLogBuffer {
		h.logBuf = h.logBuf[1:]
	}
	h.logMu.Unlock()
	h.emitLossy(chLog, e)
}

// recentLogs returns a copy of the buffered log history (oldest first).
func (h *Hub) recentLogs() []logging.Event {
	h.logMu.Lock()
	defer h.logMu.Unlock()
	return append([]logging.Event(nil), h.logBuf...)
}

// broadcastUsers sends the user list to all clients (admin UI).
func (h *Hub) broadcastUsers() {
	h.emitReliable(chUsers, h.auth.ListUsers())
}

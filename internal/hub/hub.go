package hub

import (
	"log/slog"
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

const clientSendBuffer = 256

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
	select {
	case h.broadcast <- encode(command, data):
	default:
	}
}

// --- manager.Broadcaster ---

// BroadcastFrames sends the full frames map to all clients.
func (h *Hub) BroadcastFrames(f model.Frames) { h.emitReliable(chFrames, f) }

// BroadcastGroups sends the full groups map to all clients.
func (h *Hub) BroadcastGroups(g model.Groups) { h.emitReliable(chGroups, g) }

// --- scan.Events ---

// FrameStatus reports scan progress.
func (h *Hub) FrameStatus(frameIP, status string, offline bool) {
	h.emitLossy(chFrameStatus, map[string]any{"frameIP": frameIP, "status": status, "offline": offline})
}

// SlotInfo sends a per-slot delta.
func (h *Hub) SlotInfo(frameIP string, frame *model.Frame, slotName string, slot *model.Slot) {
	h.emitLossy(chSlotInfo, slotInfoMsg{
		Frame: frameHeader{
			IP: frame.IP, Number: frame.Number, Name: frame.Name, Group: frame.Group,
			Enabled: frame.Enabled, Scan: frame.Scan, Offline: frame.Offline,
		},
		SlotName: slotName,
		Slot:     model.CloneFrame(&model.Frame{Slots: map[string]*model.Slot{slotName: slot}}).Slots[slotName],
	})
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

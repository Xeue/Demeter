// Package hub is the WebSocket transport that replaces Electron IPC. It mirrors
// the exact backend.send/backend.on contract (preload.js) with a single
// {command, data} envelope in both directions, routes the 17 inbound channels to
// the engine (with role gating + audit) and fans the 6 outbound channels to all
// connected clients via per-client write queues.
package hub

import (
	"encoding/json"

	"github.com/Xeue/Demeter/internal/model"
)

// Envelope is the on-wire message in both directions.
type Envelope struct {
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Outbound channel names.
const (
	chFrames      = "frames"
	chGroups      = "groups"
	chSlotInfo    = "slotInfo"
	chFrameStatus = "frameStatus"
	chFrameError  = "frameError"
	chLog         = "log"
	chUsers       = "users"
	chCredentials = "credentials"
	chExport      = "exportData"
)

// frameHeader is the trimmed frame sent with a per-slot slotInfo (enough for the
// front-end to lazily create the frame container).
type frameHeader struct {
	IP      string `json:"ip"`
	Number  string `json:"number"`
	Name    string `json:"name"`
	Group   string `json:"group"`
	Enabled bool   `json:"enabled"`
	Scan    bool   `json:"scan"`
	Offline bool   `json:"offline"`
}

// slotInfoMsg is the per-slot delta payload (replaces sending the whole frame +
// all slots once per slot, the O(slots^2) hotspot).
type slotInfoMsg struct {
	Frame    frameHeader `json:"frame"`
	SlotName string      `json:"slotName"`
	Slot     *model.Slot `json:"slot"`
}

func encode(command string, data any) []byte {
	d, _ := json.Marshal(data)
	b, _ := json.Marshal(Envelope{Command: command, Data: d})
	return b
}

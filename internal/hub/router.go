package hub

import (
	"encoding/json"
	"log/slog"

	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/model"
)

// Router dispatches inbound envelopes to the engine, enforcing role gates and
// writing audit records for destructive actions.
type Router struct{ h *Hub }

func newRouter(h *Hub) *Router { return &Router{h: h} }

func (r *Router) dispatch(c *Client, env Envelope) {
	h := r.h
	e := h.engine
	if e == nil {
		return
	}
	switch env.Command {

	// --- reads (any authenticated user) ---
	case "getFrames":
		c.trySend(encode(chFrames, e.FramesSnapshot()))
	case "getGroups":
		c.trySend(encode(chGroups, e.GroupsSnapshot()))
	case "getExport":
		// Authoritative, config-only snapshot for the export selection UI.
		frames, groups := e.ExportSnapshot()
		c.trySend(encode(chExport, map[string]any{"frames": frames, "groups": groups}))

	// --- frame edits (operator+) ---
	case "addFrame":
		var d struct{ IP, Number, Name, Group, Type string }
		mustJSON(env.Data, &d)
		e.AddFrame(d.IP, d.Number, d.Name, d.Group, d.Type)
	case "setCommand":
		var d struct {
			IP, Slot, Command, DataType string
			Value                       model.Value
			Enabled                     bool
			Take                        model.Num
		}
		mustJSON(env.Data, &d)
		e.SetCommand(d.IP, d.Slot, d.Command, d.Value, d.Enabled, d.DataType, d.Take)
	case "setEnable":
		var d struct {
			IP, Slot, Command, DataType string
			Enabled                     bool
			Take                        model.Num
		}
		mustJSON(env.Data, &d)
		e.SetEnable(d.IP, d.Slot, d.Command, d.Enabled, d.DataType, d.Take)
	case "scanFrame":
		var d struct {
			IP   string
			Scan bool
		}
		mustJSON(env.Data, &d)
		e.ScanFrame(d.IP, d.Scan)
	case "pollNow":
		// Operator "try again": immediate scan + blast of the frame (audited, as
		// it can re-push to live hardware).
		var d struct{ IP string }
		mustJSON(env.Data, &d)
		e.PollNow(d.IP)
		c.audit("pollNow", d)
	case "applyFrame":
		// Operator "Apply changes": one-shot force-blast of the pending diff
		// (Scan-only mode) without enabling permanent blasting. Audited.
		var d struct{ IP string }
		mustJSON(env.Data, &d)
		e.ApplyNow(d.IP)
		c.audit("applyFrame", d)
	case "stageCard":
		var d struct{ IP, Slot string }
		mustJSON(env.Data, &d)
		e.StageCard(d.IP, d.Slot)
	case "removeCard":
		var d struct{ IP, Slot string }
		mustJSON(env.Data, &d)
		e.RemoveCard(d.IP, d.Slot)

	// --- blasting toggles (operator+, audited) ---
	case "enableFrame":
		var d struct {
			IP      string
			Enabled bool
		}
		mustJSON(env.Data, &d)
		e.EnableFrame(d.IP, d.Enabled)
		c.audit("enableFrame", d)
	case "enableSlot":
		var d struct {
			IP, Slot string
			Enabled  bool
		}
		mustJSON(env.Data, &d)
		e.EnableSlot(d.IP, d.Slot, d.Enabled)
		c.audit("enableSlot", d)
	case "setAutoReboot":
		// Per-frame auto-reboot override ("", "on", "off").
		var d struct{ FrameIP, Mode string }
		mustJSON(env.Data, &d)
		e.SetAutoReboot(d.FrameIP, d.Mode)
		c.audit("setAutoReboot", d)

	// --- global policy (admin only, audited) ---
	case "setScanInterval":
		if !c.requireRole(auth.RoleAdmin) {
			c.audit("setScanInterval.denied", env.Command)
			return
		}
		var d struct{ Seconds int }
		mustJSON(env.Data, &d)
		e.SetScanInterval(d.Seconds)
		c.audit("setScanInterval", d)
	case "setGlobalAutoReboot":
		if !c.requireRole(auth.RoleAdmin) {
			c.audit("setGlobalAutoReboot.denied", env.Command)
			return
		}
		var d struct{ Enabled bool }
		mustJSON(env.Data, &d)
		e.SetGlobalAutoReboot(d.Enabled)
		c.audit("setGlobalAutoReboot", d)

	// --- destructive (admin only, audited) ---
	case "deleteFrame":
		if !c.requireRole(auth.RoleAdmin) {
			c.audit("deleteFrame.denied", env.Command)
			return
		}
		var d struct{ IP string }
		mustJSON(env.Data, &d)
		e.DeleteFrame(d.IP)
		c.audit("deleteFrame", d)
	case "cardReboot":
		if !c.requireRole(auth.RoleAdmin) {
			c.audit("cardReboot.denied", env.Command)
			return
		}
		var d struct{ FrameIP, Slot string }
		mustJSON(env.Data, &d)
		e.Reboot(d.FrameIP, d.Slot)
		c.audit("cardReboot", d)
	case "setFrames":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		var f model.Frames
		mustJSON(env.Data, &f)
		e.SetFrames(f)
		c.audit("setFrames", map[string]int{"count": len(f)})
	case "setGroups":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		var g model.Groups
		mustJSON(env.Data, &g)
		e.SetGroups(g)
		c.audit("setGroups", map[string]int{"count": len(g)})
	case "importData":
		// Granular merge import (selected frames/cards + groups). Admin only.
		if !c.requireRole(auth.RoleAdmin) {
			c.audit("importData.denied", env.Command)
			return
		}
		var d struct {
			Frames model.Frames
			Groups model.Groups
		}
		mustJSON(env.Data, &d)
		e.ImportData(d.Frames, d.Groups)
		c.audit("importData", map[string]int{"frames": len(d.Frames), "groups": len(d.Groups)})

	// --- group edits (operator+) ---
	case "addGroup":
		var d struct {
			Name    string
			Enabled bool
		}
		mustJSON(env.Data, &d)
		e.AddGroup(d.Name, d.Enabled)
	case "setGroupCommand":
		var d struct {
			Group, Type, DataType, Increment, Command string
			Value                                     model.Value
			Enabled                                   bool
			Take                                      model.Num
		}
		mustJSON(env.Data, &d)
		e.SetGroupCommand(d.Group, d.Type, d.DataType, d.Increment, d.Command, d.Value, d.Enabled, d.Take)
	case "enableGroup":
		var d struct {
			Name    string
			Enabled bool
		}
		mustJSON(env.Data, &d)
		e.EnableGroup(d.Name, d.Enabled)
	case "deleteGroup":
		var d struct{ Name string }
		mustJSON(env.Data, &d)
		e.DeleteGroup(d.Name)

	// --- user management (admin only, audited) ---
	case "getUsers":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		c.trySend(encode(chUsers, h.auth.ListUsers()))
	case "addUser":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		var d struct {
			Username, Password string
			Role               auth.Role
		}
		mustJSON(env.Data, &d)
		if d.Role == "" {
			d.Role = auth.RoleOperator
		}
		if err := h.auth.CreateUser(d.Username, d.Password, d.Role); err != nil {
			slog.Warn("addUser failed", "err", err)
		}
		c.audit("addUser", map[string]string{"username": d.Username, "role": string(d.Role)})
		h.broadcastUsers()
	case "setUserRole":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		var d struct {
			Username string
			Role     auth.Role
		}
		mustJSON(env.Data, &d)
		_ = h.auth.SetRole(d.Username, d.Role)
		c.audit("setUserRole", d)
		h.broadcastUsers()
	case "resetPassword":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		var d struct{ Username, Password string }
		mustJSON(env.Data, &d)
		_ = h.auth.ResetPassword(d.Username, d.Password)
		c.audit("resetPassword", map[string]string{"username": d.Username})
	case "setUserDisabled":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		var d struct {
			Username string
			Disabled bool
		}
		mustJSON(env.Data, &d)
		_ = h.auth.SetDisabled(d.Username, d.Disabled)
		c.audit("setUserDisabled", d)
		h.broadcastUsers()
	case "deleteUser":
		if !c.requireRole(auth.RoleAdmin) {
			return
		}
		var d struct{ Username string }
		mustJSON(env.Data, &d)
		_ = h.auth.DeleteUser(d.Username)
		c.audit("deleteUser", d)
		h.broadcastUsers()

	case "dismissNotice":
		// Acknowledge the one-time generated-credentials notice.
		h.auth.ClearNotice()

	case "window":
		// Electron window control - no-op in the headless server.

	default:
		slog.Debug("hub: unknown command", "command", env.Command)
	}
}

func mustJSON(data json.RawMessage, v any) {
	_ = json.Unmarshal(data, v)
}

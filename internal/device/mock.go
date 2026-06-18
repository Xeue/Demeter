package device

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/model"
)

// MockDialer simulates a frame full of cards for developing the GUI without
// hardware. It drives the real scan/blast path: a dialed frame IP answers the
// frame-level reads (address discovery, slot discovery, per-slot card status),
// and the synthetic card IPs answer the direct-to-card reads (IO string + the
// full parameter set). SETs are applied so blasting visibly takes effect (the
// next read echoes the new value, so controls go green).
//
// Every dialed frame IP gets its own frame with Cards populated cards in slots
// 1..Cards; the rest report "No Unit Fitted".
type MockDialer struct {
	Cards   int             // cards per frame (default 6)
	Offline map[string]bool // frame IPs that should fail to dial (simulate an unreachable frame)
	Latency time.Duration   // artificial per-batch delay (simulate real network round-trips)

	mu       sync.Mutex
	defaults map[uint32]model.Value // per-command catalogue default, shared by all cards
	frames   map[string]*mockFrame  // frame IP -> frame
	cardByIP map[string]*mockCard   // synthetic card IP -> card
	seq      int
}

const mockFrameAddr = "10" // the controller address the mock reports for every frame

type mockFrame struct {
	ip    string
	cards map[int]*mockCard // by slot number (1-based)
}

type mockCard struct {
	slot      string // hex slot token (matches what scanSlot uses)
	ip1, ip2  string
	ins, outs int
	mu        sync.Mutex
	params    map[uint32]model.Value
}

// Dial returns a device for a frame IP or one of its synthetic card IPs.
func (d *MockDialer) Dial(_ context.Context, ip string) (Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frames == nil {
		d.init()
	}
	if d.Offline[ip] {
		return nil, ErrFrameUnreachable // simulate an unreachable/offline frame
	}
	if c := d.cardByIP[ip]; c != nil {
		return &mockDevice{card: c, latency: d.Latency}, nil
	}
	f := d.frames[ip]
	if f == nil {
		f = d.buildFrame(ip)
		d.frames[ip] = f
	}
	return &mockDevice{frame: f, latency: d.Latency}, nil
}

func (d *MockDialer) init() {
	d.frames = map[string]*mockFrame{}
	d.cardByIP = map[string]*mockCard{}
	d.defaults = mockDefaults()
}

func (d *MockDialer) buildFrame(ip string) *mockFrame {
	n := d.Cards
	if n <= 0 {
		n = 6
	}
	d.seq++
	f := &mockFrame{ip: ip, cards: map[int]*mockCard{}}
	for slot := 1; slot <= n; slot++ {
		c := &mockCard{
			slot:   fmt.Sprintf("%02x", slot),
			ip1:    fmt.Sprintf("10.99.%d.%d", d.seq, slot),
			ip2:    fmt.Sprintf("10.98.%d.%d", d.seq, slot),
			ins:    8,
			outs:   8,
			params: cloneVals(d.defaults),
		}
		// Realistic, consistent status fields (read both at the frame and the card).
		c.params[4101] = model.StrVal(c.ip1)         // Ethernet-1 IP
		c.params[4201] = model.StrVal(c.ip2)         // Ethernet-2 IP
		c.params[4128] = model.StrVal("UP")          // Ethernet-1 link
		c.params[4228] = model.StrVal("Down")        // Ethernet-2 link
		c.params[4129] = model.StrVal("OK")          // SFP 1
		c.params[4229] = model.StrVal("OK")          // SFP 2
		c.params[4108] = model.IntVal(1)             // mode = Static
		c.params[18000] = model.StrVal("8 In 8 Out") // IO string
		f.cards[slot] = c
		d.cardByIP[c.ip1] = c
	}
	return f
}

// --- mock device ---

type mockDevice struct {
	frame   *mockFrame // set when this is the frame connection
	card    *mockCard  // set when this is a direct-to-card connection
	latency time.Duration
}

func (m *mockDevice) Get(_ context.Context, addr, slot string, cmd uint32) (model.Value, error) {
	if m.card != nil {
		if v, ok := m.card.get(cmd); ok {
			return v, nil
		}
		return model.None(), ErrUnitOffline
	}
	// Frame connection.
	switch {
	case addr == "00": // address discovery (cmd 17044/16482)
		if cmd == 17044 {
			return model.StrVal("Unit Addr = 0x" + mockFrameAddr), nil
		}
		return model.None(), ErrUnitOffline
	case slot == "00": // slot discovery: cmd 16530.. -> card type per slot
		slotNum := int(cmd) - 16529
		if c := m.frame.cards[slotNum]; c != nil {
			return model.StrVal("IQUCP25_SDI v1.2.3"), nil
		}
		return model.StrVal("No Unit Fitted"), nil
	default: // per-slot card status at the frame address
		if c := m.frame.cards[hexToInt(slot)]; c != nil {
			if v, ok := c.get(cmd); ok {
				return v, nil
			}
		}
		return model.None(), ErrUnitOffline
	}
}

func (m *mockDevice) BatchGet(ctx context.Context, addr, slot string, cmds []uint32) (map[uint32]model.Value, map[uint32]error) {
	if m.latency > 0 {
		select {
		case <-time.After(m.latency):
		case <-ctx.Done():
			return map[uint32]model.Value{}, map[uint32]error{}
		}
	}
	vals := make(map[uint32]model.Value, len(cmds))
	errs := map[uint32]error{}
	for _, cmd := range cmds {
		if v, err := m.Get(ctx, addr, slot, cmd); err == nil {
			vals[cmd] = v
		} else {
			errs[cmd] = err
		}
	}
	return vals, errs
}

func (m *mockDevice) Set(_ context.Context, addr, slot string, cmd uint32, v model.Value) (model.Value, error) {
	c := m.card
	if c == nil && m.frame != nil { // frame-addressed SET targets the slot's card
		c = m.frame.cards[hexToInt(slot)]
	}
	if c == nil {
		return v, nil
	}
	c.set(cmd, v)
	return v, nil // echo, like a real device
}

func (m *mockDevice) Take(context.Context, string, string, uint32) error { return nil }
func (m *mockDevice) Close() error                                       { return nil }

func (c *mockCard) get(cmd uint32) (model.Value, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.params[cmd]
	return v, ok
}

func (c *mockCard) set(cmd uint32, v model.Value) {
	c.mu.Lock()
	c.params[cmd] = v
	c.mu.Unlock()
}

func cloneVals(in map[uint32]model.Value) map[uint32]model.Value {
	out := make(map[uint32]model.Value, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func hexToInt(s string) int {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 16, 32)
	return int(n)
}

// mockDefaults builds a value for every scanned command id from the catalogue's
// per-command default (parsed from the raw JSON), falling back to a value that
// suits the command type so every GUI field populates with something sensible.
func mockDefaults() map[uint32]model.Value {
	type rawCmd struct {
		Command   uint32          `json:"command"`
		Type      string          `json:"type"`
		Increment int             `json:"increment"`
		Default   json.RawMessage `json:"default"`
		Options   json.RawMessage `json:"options"`
	}
	var raw struct {
		Card   []struct{ Commands []rawCmd } `json:"card"`
		Spigot []struct{ Commands []rawCmd } `json:"spigot"`
	}
	_ = json.Unmarshal(commandsdb.RawJSON(), &raw)

	val := func(c rawCmd) model.Value {
		if len(c.Default) > 0 && string(c.Default) != "null" {
			var n json.Number
			if json.Unmarshal(c.Default, &n) == nil {
				if i, err := n.Int64(); err == nil {
					return model.IntVal(i)
				}
			}
			var s string
			if json.Unmarshal(c.Default, &s) == nil {
				return model.StrVal(s)
			}
		}
		switch c.Type {
		case "smartip":
			return model.StrVal("0.0.0.0")
		case "text":
			return model.StrVal("")
		case "boolean", "select":
			return model.IntVal(0)
		default:
			return model.IntVal(0)
		}
	}

	out := map[uint32]model.Value{}
	for _, g := range raw.Card {
		for _, c := range g.Commands {
			out[c.Command] = val(c)
		}
	}
	for idx := 0; idx < commandsdb.Spigots; idx++ {
		for _, g := range raw.Spigot {
			for _, c := range g.Commands {
				id := c.Command
				if c.Increment != 0 {
					id = c.Command + uint32(c.Increment*idx)
				}
				out[id] = val(c)
			}
		}
	}
	return out
}

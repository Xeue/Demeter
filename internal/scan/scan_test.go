package scan

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
)

type noopEvents struct{}

func (noopEvents) FrameStatus(string, string, bool)                   {}
func (noopEvents) SlotInfo(string, *model.Frame, string, *model.Slot) {}
func (noopEvents) FrameError(string, string)                          {}

type testConns struct {
	mu          sync.Mutex
	devs        map[string]*device.FakeDevice
	unreachable map[string]bool
}

func newTestConns() *testConns {
	return &testConns{devs: map[string]*device.FakeDevice{}, unreachable: map[string]bool{}}
}

func (t *testConns) dev(ip string) *device.FakeDevice {
	t.mu.Lock()
	defer t.mu.Unlock()
	d := t.devs[ip]
	if d == nil {
		d = device.NewFakeDevice()
		t.devs[ip] = d
	}
	return d
}

// markUnreachable makes Device(ip) fail, simulating a card whose IP we cannot
// route to yet (the normal state of a card mid-reconfiguration).
func (t *testConns) markUnreachable(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.unreachable[ip] = true
}

func (t *testConns) Device(_ context.Context, ip string) (device.Device, error) {
	t.mu.Lock()
	unreachable := t.unreachable[ip]
	t.mu.Unlock()
	if unreachable {
		return nil, device.ErrFrameUnreachable
	}
	return t.dev(ip), nil
}

func grp(v model.Value, typ string, take model.Num) model.FrameGroup {
	return model.FrameGroup{Value: v, Type: typ, Enabled: true, Take: take}
}
func pref(v model.Value, typ string) model.FramePrefered {
	return model.FramePrefered{Value: v, Type: typ, Enabled: true}
}

func TestBuildCommands(t *testing.T) {
	t.Run("group equals active -> no command", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["4108"] = grp(model.IntVal(1), "select", 0)
		sl.Active["4108"] = model.IntVal(1)
		fc, cc, _, _ := buildCommands(sl, false)
		if len(fc) != 0 || len(cc) != 0 {
			t.Errorf("expected no commands, got frame=%v card=%v", fc, cc)
		}
	})

	t.Run("group differs, frame-list cmd -> frameCommands+takes", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["4108"] = grp(model.IntVal(0), "select", 4051)
		sl.Active["4108"] = model.IntVal(1)
		fc, cc, ft, _ := buildCommands(sl, false)
		if _, ok := fc["4108"]; !ok {
			t.Errorf("expected 4108 in frameCommands")
		}
		if len(cc) != 0 {
			t.Errorf("expected no card commands")
		}
		if !ft[4051] {
			t.Errorf("expected frameTake 4051")
		}
	})

	t.Run("group differs, card cmd -> cardCommands+cardTakes", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["50007"] = grp(model.IntVal(1), "select", 50002)
		sl.Active["50007"] = model.IntVal(0)
		_, cc, _, ct := buildCommands(sl, false)
		if _, ok := cc["50007"]; !ok {
			t.Errorf("expected 50007 in cardCommands")
		}
		if !ct[50002] {
			t.Errorf("expected cardTake 50002")
		}
	})

	t.Run("shuffle same index -> skipped", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["50265"] = grp(model.IntVal(1), "select", 0)
		sl.Active["50265"] = model.StrVal("All Mute") // index 1
		_, cc, _, _ := buildCommands(sl, false)
		if _, ok := cc["50265"]; ok {
			t.Errorf("shuffle with matching index should be skipped")
		}
	})

	t.Run("shuffle different index -> cardCommands shuffle", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["50265"] = grp(model.IntVal(1), "select", 0)
		sl.Active["50265"] = model.StrVal("Pass-through") // index 0
		_, cc, _, _ := buildCommands(sl, false)
		c, ok := cc["50265"]
		if !ok || c.typ != "shuffle" {
			t.Errorf("expected shuffle card command, got %v", cc)
		}
	})

	t.Run("prefered overrides group frame command", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["4108"] = grp(model.IntVal(0), "select", 0)
		sl.Active["4108"] = model.IntVal(1)
		sl.Prefered["4108"] = pref(model.IntVal(2), "select")
		fc, _, _, _ := buildCommands(sl, false)
		if fc["4108"].value.Int != 2 {
			t.Errorf("expected prefered value 2 to win, got %v", fc["4108"].value)
		}
	})

	t.Run("prefered equal active deletes queued frame command", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["4108"] = grp(model.IntVal(0), "select", 0)
		sl.Active["4108"] = model.IntVal(1)
		sl.Prefered["4108"] = pref(model.IntVal(1), "select") // == active
		fc, _, _, _ := buildCommands(sl, false)
		if _, ok := fc["4108"]; ok {
			t.Errorf("prefered==active should delete the queued frame command")
		}
	})

	t.Run("checkNull inversion sends when active absent", func(t *testing.T) {
		sl := model.NewSlot()
		sl.Group["4108"] = grp(model.IntVal(0), "select", 0)
		// no active value
		fcNull, _, _, _ := buildCommands(sl, true)
		if _, ok := fcNull["4108"]; !ok {
			t.Errorf("checkNull=true with absent active should send")
		}
		fcNorm, _, _, _ := buildCommands(sl, false)
		if _, ok := fcNorm["4108"]; ok {
			t.Errorf("checkNull=false with absent active should skip")
		}
	})
}

func TestGetFrameAddress(t *testing.T) {
	s := &Scanner{Pool: pool.New(4), Events: noopEvents{}}
	conns := newTestConns()

	conns.dev("a").Seed("00", "00", 17044, model.StrVal("unit = 0x30"))
	addr, err := s.getFrameAddress(context.Background(), conns, "a")
	if err != nil || addr != "30" {
		t.Errorf("17044 path: addr=%q err=%v want 30/nil", addr, err)
	}

	conns.dev("b").Seed("00", "00", 17044, model.StrVal("Not In Use"))
	conns.dev("b").Seed("00", "00", 16482, model.StrVal("x:01:y"))
	addr, err = s.getFrameAddress(context.Background(), conns, "b")
	if err != nil || addr != "01" {
		t.Errorf("16482 path: addr=%q err=%v want 01/nil", addr, err)
	}

	// nothing seeded -> unreachable
	if _, err := s.getFrameAddress(context.Background(), conns, "c"); err == nil {
		t.Errorf("expected error for unreachable frame")
	}
}

func TestCheckFrameEndToEnd(t *testing.T) {
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}}
	conns := newTestConns()

	const frameIP = "10.0.0.1"
	const cardIP = "10.0.0.50"
	fd := conns.dev(frameIP)
	cd := conns.dev(cardIP)

	// frame address (addr 00 slot 00)
	fd.Seed("00", "00", 17044, model.StrVal("unit = 0x10"))
	// slot discovery (addr 10 slot 00): slot 01 is a UCP
	fd.Seed("00", "00", 0, model.None()) // noop seed to ensure device exists
	fd.Seed("10", "00", 16530, model.StrVal("IQUCP25_SDI v1"))
	// card status block (addr 10 slot 01)
	fd.Seed("10", "01", 4101, model.StrVal(cardIP))
	fd.Seed("10", "01", 4128, model.StrVal("UP"))
	fd.Seed("10", "01", 4108, model.IntVal(1)) // mode currently Static(1)
	// card IO + at least one of the 298 card params (so the full read is non-empty
	// and checkCard succeeds, setting ins/outs) — addr 30 slot 00 on the card IP.
	cd.Seed("30", "00", 18000, model.StrVal("4 In 8 Out"))
	cd.Seed("30", "00", 4108, model.IntVal(1))

	frame := &model.Frame{
		IP: frameIP, Number: "5", Group: "g1", Scan: true, Enabled: true,
		Slots: map[string]*model.Slot{},
	}
	// group wants mode=0 (DHCP) on cmd 4108 (a frame-list command)
	groups := model.Groups{
		"g1": &model.Group{
			Name: "g1", Enabled: true,
			Commands: map[string]model.CommandDef{
				"4108": {Value: model.StrVal("0"), Enabled: true, Type: "card", DataType: "select"},
			},
		},
	}

	s.CheckFrame(context.Background(), frame, groups, conns)

	sl := frame.Slots["01"]
	if sl == nil {
		t.Fatal("slot 01 not discovered")
	}
	if sl.Ins != 4 || sl.Outs != 8 {
		t.Errorf("ins/outs = %d/%d want 4/8", sl.Ins, sl.Outs)
	}
	if sl.Active["4108"].Int != 1 {
		t.Errorf("active 4108 = %v want 1", sl.Active["4108"])
	}
	// mode differs (group 0 vs active 1) and 4108 is a frame command -> a SET to
	// the frame at addr 10 slot 01 must have been recorded.
	found := false
	for _, set := range fd.Sets() {
		if set.Cmd == 4108 && set.Addr == "10" && set.Slot == "01" {
			found = true
			if set.Value.Int != 0 {
				t.Errorf("blasted 4108 value = %v want 0", set.Value)
			}
		}
	}
	if !found {
		t.Errorf("expected a frame SET of 4108; sets=%v", fd.Sets())
	}
}

// TestWorkflow_UnreachableCard_SetsIPViaFrame_DefersBulk verifies the core
// programming workflow: a discovered card whose own IP we can't yet route to
// still gets its IP/mode set VIA THE FRAME, while the bulk (direct-to-card)
// settings are deferred — they are NOT misrouted through the frame, and the
// scan does not error. Once the card is rebooted onto a reachable IP, a later
// cycle pushes the bulk directly (covered by TestCheckFrameEndToEnd's path).
func TestWorkflow_UnreachableCard_SetsIPViaFrame_DefersBulk(t *testing.T) {
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}}
	conns := newTestConns()

	const frameIP = "10.0.0.1"
	const cardIP = "192.168.0.50" // card's current IP — not routable from us yet
	fd := conns.dev(frameIP)

	fd.Seed("00", "00", 17044, model.StrVal("unit = 0x10")) // frame address 10
	fd.Seed("10", "00", 16530, model.StrVal("IQUCP25_SDI v1"))
	// Card status block read via the frame: link is UP, current IP is the
	// unreachable one, mode currently Static(1).
	fd.Seed("10", "01", 4101, model.StrVal(cardIP))
	fd.Seed("10", "01", 4128, model.StrVal("UP"))
	fd.Seed("10", "01", 4108, model.IntVal(1))
	// The card's own IP is unreachable -> checkCard + the direct bulk push fail.
	conns.markUnreachable(cardIP)

	frame := &model.Frame{
		IP: frameIP, Number: "5", Group: "g1", Scan: true, Enabled: true,
		Slots: map[string]*model.Slot{},
	}
	// Group wants: new IP on 4101 (a FRAME-routed command) and a bulk card
	// setting on 50007 (a direct-to-card command).
	groups := model.Groups{
		"g1": &model.Group{
			Name: "g1", Enabled: true,
			Commands: map[string]model.CommandDef{
				"4101":  {Value: model.StrVal("10.40.0.50"), Enabled: true, Type: "card", DataType: "select"},
				"50007": {Value: model.StrVal("1"), Enabled: true, Type: "card", DataType: "select"},
			},
		},
	}

	s.CheckFrame(context.Background(), frame, groups, conns)

	if frame.Slots["01"] == nil {
		t.Fatal("slot 01 not discovered")
	}

	// 4101 (new IP) must have been SET via the frame (addr 10, slot 01).
	gotIPViaFrame := false
	for _, set := range fd.Sets() {
		if set.Cmd == 4101 && set.Addr == "10" && set.Slot == "01" {
			gotIPViaFrame = true
			if set.Value.String() != "10.40.0.50" {
				t.Errorf("frame-set 4101 = %q want 10.40.0.50", set.Value.String())
			}
		}
		// The bulk command must NOT be misrouted through the frame.
		if set.Cmd == 50007 {
			t.Errorf("bulk command 50007 was sent via the frame; should be direct-to-card only")
		}
	}
	if !gotIPViaFrame {
		t.Errorf("expected IP (4101) to be set via the frame; frame sets=%v", fd.Sets())
	}

	// No device was ever created for the unreachable card IP (bulk deferred).
	conns.mu.Lock()
	_, dialedCard := conns.devs[cardIP]
	conns.mu.Unlock()
	if dialedCard {
		t.Errorf("a connection to the unreachable card IP %s should not have succeeded", cardIP)
	}

	// The IP change is restart-required, so the slot must report reboot-needed
	// with a reason naming the IP command (for the GUI badge + tooltip).
	sl := frame.Slots["01"]
	if !sl.RebootNeeded {
		t.Errorf("expected RebootNeeded after blasting a restart-required IP change")
	}
	foundReason := false
	for _, r := range sl.RebootReasons {
		if strings.Contains(r, "(4101)") && strings.Contains(r, "10.40.0.50") {
			foundReason = true
		}
	}
	if !foundReason {
		t.Errorf("expected a reboot reason naming 4101 -> 10.40.0.50; got %v", sl.RebootReasons)
	}
}

func TestRestartNames(t *testing.T) {
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	names := db.RestartNames()
	// The 8 IP/mode commands plus the two ST2110-30 disables are restart-flagged.
	for _, id := range []uint32{4101, 4103, 4105, 4108, 4201, 4203, 4205, 4208, 48729, 48730} {
		if _, ok := names[id]; !ok {
			t.Errorf("expected command %d to be restart-flagged", id)
		}
	}
	// A non-restart command must not be present.
	if _, ok := names[18000]; ok {
		t.Errorf("18000 (IO string) should not be restart-flagged")
	}
}

func TestRebootReasonsHelper(t *testing.T) {
	restart := map[uint32]string{4101: "IP"}
	sent := map[string]sendCmd{
		"4101":  {value: model.StrVal("10.0.0.9"), typ: "smartip"},
		"50007": {value: model.IntVal(1), typ: "select"}, // not restart-flagged
	}
	active := map[string]model.Value{"4101": model.StrVal("10.0.0.1")}
	got := rebootReasons(sent, active, restart)
	if len(got) != 1 || !strings.Contains(got[0], "IP (4101)") || !strings.Contains(got[0], "10.0.0.1 → 10.0.0.9") {
		t.Errorf("unexpected reasons: %v", got)
	}
}

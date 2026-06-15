package frame

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
	"github.com/Xeue/Demeter/internal/scan"
)

type noopEvents struct{}

func (noopEvents) FrameStatus(string, string, bool)                   {}
func (noopEvents) SlotInfo(string, *model.Frame, string, *model.Slot) {}
func (noopEvents) FrameError(string, string)                          {}

func seededDialer(t *testing.T, getDelay time.Duration) *device.FakeDialer {
	t.Helper()
	const frameIP = "10.0.0.1"
	const cardIP = "10.0.0.50"
	fd := device.NewFakeDialer()
	frameDev := device.NewFakeDevice()
	cardDev := device.NewFakeDevice()
	frameDev.GetDelay = getDelay
	cardDev.GetDelay = getDelay
	fd.Devices[frameIP] = frameDev
	fd.Devices[cardIP] = cardDev

	frameDev.Seed("00", "00", 17044, model.StrVal("unit = 0x10"))
	frameDev.Seed("10", "00", 16530, model.StrVal("IQUCP25_SDI v1"))
	frameDev.Seed("10", "01", 4101, model.StrVal(cardIP))
	frameDev.Seed("10", "01", 4128, model.StrVal("UP"))
	frameDev.Seed("10", "01", 4108, model.IntVal(1))
	cardDev.Seed("30", "00", 18000, model.StrVal("4 In 8 Out"))
	cardDev.Seed("30", "00", 4108, model.IntVal(1))
	return fd
}

func newTestActor(t *testing.T, dialer device.Dialer, getDelay time.Duration) (*Actor, func() *model.Frame) {
	t.Helper()
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	sc := &scan.Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}}

	var mu sync.Mutex
	var last *model.Frame
	deps := Deps{
		Scanner:  sc,
		Dialer:   dialer,
		GroupsFn: func() model.Groups { return model.Groups{} },
		OnChange: func(_ string, snap *model.Frame) { mu.Lock(); last = snap; mu.Unlock() },
		Save:     func() {},
	}
	f := &model.Frame{IP: "10.0.0.1", Number: "5", Scan: true, Enabled: false, Slots: map[string]*model.Slot{}}
	a := New(f, deps)
	getLast := func() *model.Frame {
		mu.Lock()
		defer mu.Unlock()
		return model.CloneFrame(last)
	}
	return a, getLast
}

// TestActorEditPreservedDuringScan is the scan-loop-race regression: a prefered
// edit applied while a scan is in flight must survive the scan-result merge.
func TestActorEditPreservedDuringScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, getLast := newTestActor(t, seededDialer(t, time.Millisecond), time.Millisecond)
	a.Start(ctx) // triggers an immediate (slow) scan

	time.Sleep(20 * time.Millisecond) // let the scan get going
	a.SetCommand("01", "4101", model.StrVal("10.9.9.9"), true, "smartip", 0)

	// wait until the scan has merged (active populated)
	deadline := time.Now().Add(3 * time.Second)
	var snap *model.Frame
	for time.Now().Before(deadline) {
		snap = getLast()
		if snap != nil && snap.Slots["01"] != nil && len(snap.Slots["01"].Active) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.Stop()

	if snap == nil || snap.Slots["01"] == nil {
		t.Fatal("slot 01 never scanned")
	}
	sl := snap.Slots["01"]
	if len(sl.Active) == 0 {
		t.Error("scan did not populate active (merge lost scanned data)")
	}
	p, ok := sl.Prefered["4101"]
	if !ok {
		t.Fatal("prefered edit was lost across the scan merge")
	}
	if p.Value.Str != "10.9.9.9" {
		t.Errorf("prefered value = %q want 10.9.9.9", p.Value.Str)
	}
}

// autoRebootActor builds a started actor that is blasting a restart-required
// change (mode 4108: Static→DHCP) so a reboot becomes needed, with the given
// auto-reboot override and global default.
func autoRebootActor(t *testing.T, ctx context.Context, override string, defaultOn bool) (*Actor, *device.FakeDevice, func() []string) {
	t.Helper()
	dialer := seededDialer(t, 0)
	frameDev := dialer.Devices["10.0.0.1"]

	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	sc := &scan.Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}}

	var mu sync.Mutex
	var audits []string
	deps := Deps{
		Scanner: sc, Dialer: dialer,
		GroupsFn: func() model.Groups {
			return model.Groups{"g1": &model.Group{Name: "g1", Enabled: true, Commands: map[string]model.CommandDef{
				"4108": {Value: model.StrVal("0"), Enabled: true, Type: "card", DataType: "select"},
			}}}
		},
		OnChange:           func(string, *model.Frame) {},
		Save:               func() {},
		AutoRebootDefault:  func() bool { return defaultOn },
		AutoRebootCooldown: time.Minute,
		Audit:              func(action string, _ any) { mu.Lock(); audits = append(audits, action); mu.Unlock() },
	}
	f := &model.Frame{IP: "10.0.0.1", Number: "5", Group: "g1", Scan: true, Enabled: true, AutoReboot: override, Slots: map[string]*model.Slot{}}
	a := New(f, deps)
	a.Start(ctx)
	getAudits := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), audits...) }
	return a, frameDev, getAudits
}

func rebootFired(dev *device.FakeDevice) bool {
	for _, s := range dev.Sets() {
		if s.Cmd == 4114 {
			return true
		}
	}
	return false
}

// TestActorAutoRebootFires: with the global default on, a restart-required
// change triggers exactly one auto-reboot (4114) and an audit record.
func TestActorAutoRebootFires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, frameDev, getAudits := autoRebootActor(t, ctx, "", true)
	defer a.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !rebootFired(frameDev) {
		time.Sleep(10 * time.Millisecond)
	}
	if !rebootFired(frameDev) {
		t.Fatal("expected an auto-reboot (4114) to fire for a restart-required change")
	}
	found := false
	for _, action := range getAudits() {
		if action == "autoReboot" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an autoReboot audit record; got %v", getAudits())
	}
}

// TestActorAutoRebootOffOverride: a per-frame "off" override suppresses
// auto-reboot even when the global default is on.
func TestActorAutoRebootOffOverride(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, frameDev, _ := autoRebootActor(t, ctx, "off", true)
	defer a.Stop()

	time.Sleep(400 * time.Millisecond) // allow a scan + blast to complete
	if rebootFired(frameDev) {
		t.Errorf("auto-reboot fired despite a per-frame \"off\" override")
	}
}

// TestActorAutoRebootOnOverride: a per-frame "on" override fires even when the
// global default is off.
func TestActorAutoRebootOnOverride(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, frameDev, _ := autoRebootActor(t, ctx, "on", false)
	defer a.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !rebootFired(frameDev) {
		time.Sleep(10 * time.Millisecond)
	}
	if !rebootFired(frameDev) {
		t.Fatal("expected auto-reboot to fire with a per-frame \"on\" override")
	}
}

// TestActorImportFrameMergesConfig: ImportFrame restores config (group, per-card
// prefered, staged) into an existing actor without turning blasting on.
func TestActorImportFrameMergesConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _ := newTestActor(t, seededDialer(t, 0), 0) // frame starts Enabled:false
	a.Start(ctx)
	defer a.Stop()

	a.ImportFrame(&model.Frame{
		IP: "10.0.0.1", Number: "9", Name: "X", Group: "g2", Type: "ucp",
		Enabled: true, Scan: true, // import requests blasting — must be ignored
		Slots: map[string]*model.Slot{
			"02": {Enabled: true, Staged: true, Prefered: map[string]model.FramePrefered{
				"4108": {Value: model.IntVal(0), Enabled: true, Type: "select"},
			}},
		},
	})

	snap := a.Snapshot()
	if snap == nil {
		t.Fatal("nil snapshot")
	}
	if snap.Group != "g2" {
		t.Errorf("group = %q, want g2", snap.Group)
	}
	if snap.Enabled {
		t.Error("ImportFrame must not turn blasting on")
	}
	sl := snap.Slots["02"]
	if sl == nil || !sl.Staged || sl.Prefered["4108"].Value.Int != 0 {
		t.Errorf("slot 02 config not merged: %+v", sl)
	}
}

// TestActorStopMidScan ensures a delete/stop during an in-flight scan returns
// promptly without deadlock or panic (run with -race).
func TestActorStopMidScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _ := newTestActor(t, seededDialer(t, 5*time.Millisecond), 5*time.Millisecond)
	a.Start(ctx)
	time.Sleep(15 * time.Millisecond) // scan in flight

	done := make(chan struct{})
	go func() { a.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return during an in-flight scan")
	}
}

package scan

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
)

// stagedDevice models a card whose take-required settings only go ACTIVE after a
// take: Set writes to a staging area (and echoes the staged value, like a real
// device), and Get returns the active value. Take commits staging->active only
// when commitOnTake is true, so we can simulate a take that does and doesn't work.
type stagedDevice struct {
	active       map[uint32]model.Value
	staged       map[uint32]model.Value
	commitOnTake bool
	takes        int
}

func newStagedDevice(commitOnTake bool, seed map[uint32]model.Value) *stagedDevice {
	active := map[uint32]model.Value{}
	for k, v := range seed {
		active[k] = v
	}
	return &stagedDevice{active: active, staged: map[uint32]model.Value{}, commitOnTake: commitOnTake}
}

func (d *stagedDevice) Get(_ context.Context, _, _ string, cmd uint32) (model.Value, error) {
	if v, ok := d.active[cmd]; ok {
		return v, nil
	}
	return model.None(), device.ErrUnitOffline
}

func (d *stagedDevice) BatchGet(ctx context.Context, addr, slot string, cmds []uint32) (map[uint32]model.Value, map[uint32]error) {
	vals := map[uint32]model.Value{}
	errs := map[uint32]error{}
	for _, c := range cmds {
		if v, err := d.Get(ctx, addr, slot, c); err == nil {
			vals[c] = v
		} else {
			errs[c] = err
		}
	}
	return vals, errs
}

func (d *stagedDevice) Set(_ context.Context, _, _ string, cmd uint32, v model.Value) (model.Value, error) {
	d.staged[cmd] = v
	return v, nil // echo the staged value, as a real device does
}

func (d *stagedDevice) Take(_ context.Context, _, _ string, _ uint32) error {
	d.takes++
	if d.commitOnTake {
		for k, v := range d.staged {
			d.active[k] = v
		}
		d.staged = map[uint32]model.Value{}
	}
	return nil
}

func (d *stagedDevice) Close() error { return nil }

type oneDevConns struct{ dev device.Device }

func (c oneDevConns) Device(context.Context, string) (device.Device, error) { return c.dev, nil }

func newTakeScanner() *Scanner {
	return &Scanner{Pool: pool.New(4), Events: noopEvents{}, VerifyAttempts: 3, VerifyDelay: time.Nanosecond}
}

// Packet Timing (58645, take 50002): the SET stages, the take commits. Demeter
// must report it applied only after confirming it is active post-take.
func TestTakeRequiredAppliesAfterCommit(t *testing.T) {
	dev := newStagedDevice(true, map[uint32]model.Value{58645: model.IntVal(3)}) // currently 500us
	s := newTakeScanner()
	cmds := map[string]sendCmd{"58645": {value: model.IntVal(1), typ: "select"}}
	applied, failed := s.doCommands(context.Background(), oneDevConns{dev}, cmds, map[uint32]bool{50002: true}, "10.0.0.50", "30", "00")

	if dev.takes == 0 {
		t.Fatal("take was never fired")
	}
	if len(failed) != 0 {
		t.Fatalf("expected no failures, got %v", failed)
	}
	if got, ok := applied["58645"]; !ok || !model.ValuesEqualLoose(got, model.IntVal(1)) {
		t.Fatalf("applied[58645] = %v (ok=%v), want 1 (confirmed active after take)", applied["58645"], ok)
	}
}

// If the take does not commit (broken/ineffective take), the value never goes
// active: Demeter must NOT report it applied - it must flag it failed with the
// device-actual (old) value, so the row stays pending instead of falsely clearing.
func TestTakeRequiredFlaggedWhenTakeDoesNotCommit(t *testing.T) {
	dev := newStagedDevice(false, map[uint32]model.Value{58645: model.IntVal(3)}) // stays 500us
	s := newTakeScanner()
	cmds := map[string]sendCmd{"58645": {value: model.IntVal(1), typ: "select"}}
	applied, failed := s.doCommands(context.Background(), oneDevConns{dev}, cmds, map[uint32]bool{50002: true}, "10.0.0.50", "30", "00")

	if dev.takes == 0 {
		t.Fatal("take was never fired")
	}
	reason, ok := failed["58645"]
	if !ok {
		t.Fatalf("expected 58645 in failed, got applied=%v failed=%v", applied, failed)
	}
	if !strings.Contains(reason, "not active after take") {
		t.Errorf("failure reason = %q, want it to mention the take did not commit", reason)
	}
	// active must be reconciled to the device-actual (old) value so the UI stays red.
	if got, ok := applied["58645"]; !ok || !model.ValuesEqualLoose(got, model.IntVal(3)) {
		t.Errorf("applied[58645] = %v (ok=%v), want the unchanged device value 3", applied["58645"], ok)
	}
}

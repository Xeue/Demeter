package scan

import (
	"context"
	"sync"
	"testing"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
)

// mockConns adapts a device.Dialer to scan.Conns with a tiny per-IP cache.
type mockConns struct {
	d     device.Dialer
	mu    sync.Mutex
	cache map[string]device.Device
}

func (c *mockConns) Device(ctx context.Context, ip string) (device.Device, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]device.Device{}
	}
	if d := c.cache[ip]; d != nil {
		return d, nil
	}
	d, err := c.d.Dial(ctx, ip)
	if err != nil {
		return nil, err
	}
	c.cache[ip] = d
	return d, nil
}

// TestScanAgainstMock: a real CheckFrame run against the MockDialer discovers the
// expected number of cards, populates their IPs and parameter values, and a SET
// (blast) is echoed back so the value sticks.
func TestScanAgainstMock(t *testing.T) {
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}}
	conns := &mockConns{d: &device.MockDialer{Cards: 4}}

	frame := &model.Frame{IP: "10.0.0.1", Number: "1", Scan: true, Slots: map[string]*model.Slot{}}
	s.CheckFrame(context.Background(), frame, model.Groups{}, conns, false)

	online := 0
	for _, sl := range frame.Slots {
		if sl != nil && !sl.Offline {
			online++
		}
	}
	if online != 4 {
		t.Errorf("expected 4 online cards, got %d", online)
	}

	sl := frame.Slots["01"]
	if sl == nil || sl.IPA == nil {
		t.Fatal("slot 01 not populated with an IP")
	}
	if sl.Ins != 8 || sl.Outs != 8 {
		t.Errorf("slot 01 ins/outs = %d/%d, want 8/8", sl.Ins, sl.Outs)
	}
	if len(sl.Active) == 0 {
		t.Error("slot 01 has no active parameter values")
	}

	// A SET to the card is echoed (so blasting converges to green in the GUI).
	dev, _ := conns.Device(context.Background(), *sl.IPA)
	got, err := dev.Set(context.Background(), "30", "00", 4108, model.IntVal(0))
	if err != nil || got.Int != 0 {
		t.Errorf("mock Set echo = %v, %v; want int(0)", got, err)
	}
}

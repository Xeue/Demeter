package frame

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
	"github.com/Xeue/Demeter/internal/scan"
)

// countingEvents counts scans by their opening "Connecting to frame" status.
type countingEvents struct{ scans atomic.Int32 }

func (c *countingEvents) FrameStatus(_, status string, _ bool) {
	if status == "Connecting to frame" {
		c.scans.Add(1)
	}
}
func (c *countingEvents) SlotInfo(string, *model.Frame, string, *model.Slot) {}
func (c *countingEvents) FrameError(string, string)                          {}

// TestActorPollNowRetriesAfterInflight: an operator "try again" (PollNow) issued
// while a scan is already running must not be dropped — it runs a fresh scan as
// soon as the in-flight one finishes (the rescan-queued path).
func TestActorPollNowRetriesAfterInflight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	ev := &countingEvents{}
	sc := &scan.Scanner{DB: db, Pool: pool.New(8), Events: ev}
	deps := Deps{
		Scanner:  sc,
		Dialer:   seededDialer(t, time.Millisecond), // slow scan (~hundreds of 1ms gets)
		GroupsFn: func() model.Groups { return model.Groups{} },
		OnChange: func(string, *model.Frame) {},
		Save:     func() {},
	}
	f := &model.Frame{IP: "10.0.0.1", Number: "5", Scan: true, Enabled: false, Slots: map[string]*model.Slot{}}
	a := New(f, deps)
	a.Start(ctx) // scan #1 begins (slow)
	defer a.Stop()

	time.Sleep(10 * time.Millisecond) // scan #1 is in flight
	a.PollNow()                       // must be queued, not dropped

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && ev.scans.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := ev.scans.Load(); got < 2 {
		t.Fatalf("expected >=2 scans (initial + queued retry), got %d", got)
	}
}

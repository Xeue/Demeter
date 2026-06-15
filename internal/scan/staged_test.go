package scan

import (
	"context"
	"testing"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
)

// stagedFrame builds a frame with slot 01 pre-staged (Staged=true) carrying a
// per-card prefered override on 4108 that differs from what the card reports.
func stagedFrame() *model.Frame {
	sl := model.NewSlot()
	sl.Staged = true
	sl.Enabled = true
	sl.Prefered["4108"] = pref(model.IntVal(0), "select") // want 0, card reports 1
	return &model.Frame{
		IP: "10.0.0.1", Number: "5", Scan: true, Enabled: true,
		Slots: map[string]*model.Slot{"01": sl},
	}
}

// TestStagedCardAppliedOnDiscovery: a card pre-staged while offline is blasted
// (and its Staged flag cleared) the moment discovery finds a real card there.
func TestStagedCardAppliedOnDiscovery(t *testing.T) {
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}}
	conns := newTestConns()
	const frameIP, cardIP = "10.0.0.1", "10.0.0.50"
	fd, cd := conns.dev(frameIP), conns.dev(cardIP)

	fd.Seed("00", "00", 17044, model.StrVal("unit = 0x10"))
	fd.Seed("10", "00", 16530, model.StrVal("IQUCP25_SDI v1")) // slot 01 now present
	fd.Seed("10", "01", 4101, model.StrVal(cardIP))
	fd.Seed("10", "01", 4128, model.StrVal("UP"))
	fd.Seed("10", "01", 4108, model.IntVal(1)) // card currently 1; staged wants 0
	cd.Seed("30", "00", 18000, model.StrVal("4 In 8 Out"))
	cd.Seed("30", "00", 4108, model.IntVal(1))

	frame := stagedFrame()
	s.CheckFrame(context.Background(), frame, model.Groups{}, conns)

	sl := frame.Slots["01"]
	if sl == nil {
		t.Fatal("slot 01 missing")
	}
	if sl.Staged {
		t.Errorf("Staged should be cleared once a real card is discovered")
	}
	found := false
	for _, set := range fd.Sets() {
		if set.Cmd == 4108 && set.Addr == "10" && set.Slot == "01" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the staged prefered 4108 to be blasted to the frame; sets=%v", fd.Sets())
	}
}

// TestStagedCardNotBlastedWhileAbsent: while the card is not present, the staged
// slot is preserved (Staged stays true) and nothing is blasted to it.
func TestStagedCardNotBlastedWhileAbsent(t *testing.T) {
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}}
	conns := newTestConns()
	const frameIP = "10.0.0.1"
	fd := conns.dev(frameIP)
	fd.Seed("00", "00", 17044, model.StrVal("unit = 0x10")) // frame reachable, but no card seeded

	frame := stagedFrame()
	s.CheckFrame(context.Background(), frame, model.Groups{}, conns)

	sl := frame.Slots["01"]
	if sl == nil {
		t.Fatal("staged slot 01 should be preserved")
	}
	if !sl.Staged {
		t.Errorf("Staged should remain true while the card is absent")
	}
	for _, set := range fd.Sets() {
		if set.Slot == "01" {
			t.Errorf("nothing should be blasted to an absent staged card; got %v", set)
		}
	}
}

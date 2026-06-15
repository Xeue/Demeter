package scan

import (
	"context"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
)

// verifyFixture builds the common setup: a reachable frame with a UCP in slot 01
// whose card reports mode 4108=1, and a group that wants 4108=0 (a frame command,
// so it is blasted to the frame at addr 10 slot 01).
func verifyFixture(t *testing.T) (*Scanner, *testConns, *model.Frame, model.Groups) {
	t.Helper()
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &Scanner{DB: db, Pool: pool.New(8), Events: noopEvents{}, VerifyAttempts: 3, VerifyDelay: time.Nanosecond}
	conns := newTestConns()
	const frameIP, cardIP = "10.0.0.1", "10.0.0.50"
	fd, cd := conns.dev(frameIP), conns.dev(cardIP)
	fd.Seed("00", "00", 17044, model.StrVal("unit = 0x10"))
	fd.Seed("10", "00", 16530, model.StrVal("IQUCP25_SDI v1"))
	fd.Seed("10", "01", 4101, model.StrVal(cardIP))
	fd.Seed("10", "01", 4128, model.StrVal("UP"))
	fd.Seed("10", "01", 4108, model.IntVal(1)) // card currently 1
	cd.Seed("30", "00", 18000, model.StrVal("4 In 8 Out"))
	cd.Seed("30", "00", 4108, model.IntVal(1))

	frame := &model.Frame{
		IP: frameIP, Number: "5", Group: "g1", Scan: true, Enabled: true,
		Slots: map[string]*model.Slot{},
	}
	groups := model.Groups{"g1": &model.Group{
		Name: "g1", Enabled: true,
		Commands: map[string]model.CommandDef{
			"4108": {Value: model.StrVal("0"), Enabled: true, Type: "card", DataType: "select"},
		},
	}}
	return s, conns, frame, groups
}

func countSets(fd *device.FakeDevice, addr, slot string, cmd uint32) int {
	n := 0
	for _, st := range fd.Sets() {
		if st.Cmd == cmd && st.Addr == addr && st.Slot == slot {
			n++
		}
	}
	return n
}

// TestBlastVerifyRetryAndFail: a device that refuses to apply a SET is retried
// VerifyAttempts times and ends up flagged in slot.Failed.
func TestBlastVerifyRetryAndFail(t *testing.T) {
	s, conns, frame, groups := verifyFixture(t)
	fd := conns.dev("10.0.0.1")
	fd.RejectSet("10", "01", 4108) // device will not apply 4108

	s.CheckFrame(context.Background(), frame, groups, conns)

	if got := countSets(fd, "10", "01", 4108); got != 3 {
		t.Errorf("expected 4108 to be retried 3 times, got %d sends", got)
	}
	sl := frame.Slots["01"]
	if sl == nil || sl.Failed["4108"] == "" {
		t.Fatalf("expected slot.Failed[4108] to be set; failed=%v", sl.Failed)
	}
	t.Logf("failure reason: %s", sl.Failed["4108"])
}

// TestBlastVerifySucceedsFirstTry: a normal device applies the SET, so it is sent
// once and nothing is flagged.
func TestBlastVerifySucceedsFirstTry(t *testing.T) {
	s, conns, frame, groups := verifyFixture(t)
	fd := conns.dev("10.0.0.1")

	s.CheckFrame(context.Background(), frame, groups, conns)

	if got := countSets(fd, "10", "01", 4108); got != 1 {
		t.Errorf("expected 4108 to be sent once on success, got %d", got)
	}
	if sl := frame.Slots["01"]; sl != nil && len(sl.Failed) != 0 {
		t.Errorf("expected no failures, got %v", sl.Failed)
	}
}

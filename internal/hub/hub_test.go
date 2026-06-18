package hub

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Xeue/Demeter/internal/logging"
	"github.com/Xeue/Demeter/internal/model"
)

// TestLogBufferBounded: Log keeps a bounded ring of recent events (oldest
// dropped) and recentLogs returns them oldest-first for replay on connect.
func TestLogBufferBounded(t *testing.T) {
	h := New(nil)
	total := maxLogBuffer + 10
	for i := 0; i < total; i++ {
		h.Log(logging.Event{Message: fmt.Sprintf("m%d", i)})
	}
	got := h.recentLogs()
	if len(got) != maxLogBuffer {
		t.Fatalf("recentLogs len = %d, want %d", len(got), maxLogBuffer)
	}
	// The 10 oldest were dropped, so the first retained is m10 and the last is the newest.
	if got[0].Message != fmt.Sprintf("m%d", total-maxLogBuffer) {
		t.Errorf("oldest retained = %q, want m%d", got[0].Message, total-maxLogBuffer)
	}
	if got[len(got)-1].Message != fmt.Sprintf("m%d", total-1) {
		t.Errorf("newest = %q, want m%d", got[len(got)-1].Message, total-1)
	}
}

// TestSlotInfoDedup: the per-cycle re-emit of an UNCHANGED slot is suppressed,
// while a real change (and a different slot) is still broadcast. This is what
// stops a large fleet's redundant ~20KB-per-slot firehose from swamping and
// disconnecting a busy browser (which dropped the later frames' updates).
func TestSlotInfoDedup(t *testing.T) {
	h := New(nil)
	frame := &model.Frame{IP: "10.0.0.1", Number: "1"}
	slot := model.NewSlot()
	slot.Ins, slot.Outs = 4, 8

	h.SlotInfo("10.0.0.1", frame, "01", slot)
	if got := len(h.broadcast); got != 1 {
		t.Fatalf("first slotInfo should broadcast; queued=%d", got)
	}
	// Identical content next cycle -> suppressed.
	h.SlotInfo("10.0.0.1", frame, "01", slot)
	if got := len(h.broadcast); got != 1 {
		t.Errorf("unchanged slotInfo should be deduped; queued=%d (want 1)", got)
	}
	// A real change -> broadcast.
	slot.Ins = 8
	h.SlotInfo("10.0.0.1", frame, "01", slot)
	if got := len(h.broadcast); got != 2 {
		t.Errorf("changed slotInfo should broadcast; queued=%d (want 2)", got)
	}
	// A different slot is independent -> broadcast.
	h.SlotInfo("10.0.0.1", frame, "02", model.NewSlot())
	if got := len(h.broadcast); got != 3 {
		t.Errorf("a different slot should broadcast; queued=%d (want 3)", got)
	}
	// Deleting the frame prunes its dedup entries (no leak).
	h.pruneDedup(model.Frames{})
	h.dedupMu.Lock()
	n := len(h.lastSlot) + len(h.lastStatus)
	h.dedupMu.Unlock()
	if n != 0 {
		t.Errorf("dedup caches should be pruned for absent frames; entries=%d", n)
	}
}

// TestFrameStatusDedup: an unchanged status is not re-broadcast, but a flip of
// the offline flag is (so the rail's online/offline square stays correct).
func TestFrameStatusDedup(t *testing.T) {
	h := New(nil)
	h.FrameStatus("10.0.0.7", "Connecting to frame", false)
	h.FrameStatus("10.0.0.7", "Connecting to frame", false) // identical -> suppressed
	if got := len(h.broadcast); got != 1 {
		t.Errorf("unchanged frameStatus should be deduped; queued=%d (want 1)", got)
	}
	h.FrameStatus("10.0.0.7", "Cannot reach frame", true) // offline flip -> sent
	if got := len(h.broadcast); got != 2 {
		t.Errorf("offline-flag change should broadcast; queued=%d (want 2)", got)
	}
}

// TestSlotInfoBatchCoalesces: a frame's slots are emitted as ONE batch message,
// per-slot deduped, with an unchanged batch suppressed entirely - this is the
// per-frame coalescing that collapses a fleet-discovery burst from N messages to
// one per frame so a busy client's send queue can't overflow.
func TestSlotInfoBatchCoalesces(t *testing.T) {
	h := New(nil)
	frame := &model.Frame{IP: "10.0.0.1", Number: "1", Slots: map[string]*model.Slot{}}
	for _, n := range []string{"01", "02", "03"} {
		sl := model.NewSlot()
		sl.Ins, sl.Outs = 8, 8
		frame.Slots[n] = sl
	}
	names := []string{"01", "02", "03"}

	// First discovery: all 3 slots change -> ONE message carrying 3 items.
	h.SlotInfoBatch("10.0.0.1", frame, names)
	if got := len(h.broadcast); got != 1 {
		t.Fatalf("expected 1 coalesced batch, got %d messages", got)
	}
	var env Envelope
	if err := json.Unmarshal(<-h.broadcast, &env); err != nil || env.Command != chSlotInfoBatch {
		t.Fatalf("command=%q (err %v), want %q", env.Command, err, chSlotInfoBatch)
	}
	var batch slotInfoBatchMsg
	_ = json.Unmarshal(env.Data, &batch)
	if len(batch.Slots) != 3 {
		t.Errorf("batch carried %d slots, want 3", len(batch.Slots))
	}

	// Re-scan with nothing changed -> no message at all.
	h.SlotInfoBatch("10.0.0.1", frame, names)
	if got := len(h.broadcast); got != 0 {
		t.Errorf("unchanged batch should emit nothing, got %d", got)
	}

	// One slot changes -> a batch with ONLY that slot (per-item dedup).
	frame.Slots["02"].Ins = 4
	h.SlotInfoBatch("10.0.0.1", frame, names)
	if got := len(h.broadcast); got != 1 {
		t.Fatalf("expected 1 message after one slot changed, got %d", got)
	}
	_ = json.Unmarshal(<-h.broadcast, &env)
	_ = json.Unmarshal(env.Data, &batch)
	if len(batch.Slots) != 1 || batch.Slots[0].SlotName != "02" {
		t.Errorf("delta batch should carry only slot 02, got %+v", batch.Slots)
	}
}

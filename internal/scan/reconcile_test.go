package scan

import (
	"context"
	"testing"
)

// TestBlastReconcilesActiveFromEcho: after a successful blast, the slot's active
// map is reconciled from the device's SET echo to the just-applied value, so the
// next scan sees no diff (idempotent) and the UI's pending counts can drop now
// instead of waiting for a full re-read.
func TestBlastReconcilesActiveFromEcho(t *testing.T) {
	s, conns, frame, groups := verifyFixture(t) // group wants 4108=0; card reports 4108=1
	fd := conns.dev("10.0.0.1")

	s.CheckFrame(context.Background(), frame, groups, conns, false)

	if got := frame.Slots["01"].Active["4108"]; got.Int != 0 {
		t.Fatalf("after blast active 4108 = %v, want 0 (reconciled from echo)", got)
	}
	first := countSets(fd, "10", "01", 4108)
	if first == 0 {
		t.Fatal("expected 4108 to be blasted on the first pass")
	}

	// Second pass: active now matches the target, so 4108 must NOT be re-blasted.
	s.CheckFrame(context.Background(), frame, groups, conns, false)
	if got := countSets(fd, "10", "01", 4108); got != first {
		t.Errorf("4108 re-blasted on the second pass (%d -> %d); active reconcile should make it idempotent", first, got)
	}
}

// TestRejectedSetActiveStaysPending: when the device rejects a SET (echo never
// matches), active becomes the device's actual value (still != target), so the
// row correctly stays pending and the command is reported failed.
func TestRejectedSetActiveStaysPending(t *testing.T) {
	s, conns, frame, groups := verifyFixture(t)
	fd := conns.dev("10.0.0.1")
	fd.RejectSet("10", "01", 4108) // device refuses to apply 4108

	s.CheckFrame(context.Background(), frame, groups, conns, false)

	sl := frame.Slots["01"]
	if got := sl.Active["4108"]; got.Int != 1 {
		t.Errorf("rejected: active 4108 = %v, want the device-actual 1 (stays != target 0)", got)
	}
	if sl.Failed["4108"] == "" {
		t.Errorf("rejected SET should be reported in sl.Failed; got %v", sl.Failed)
	}
}

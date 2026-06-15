package manager

import (
	"testing"

	"github.com/Xeue/Demeter/internal/model"
)

// TestSetScanInterval: the global scan interval is settable at runtime, clamped
// to [1,3600]s, persisted via the callback, and readable back.
func TestSetScanInterval(t *testing.T) {
	m, cancel := testManager(t, model.Frames{}, model.Groups{})
	defer cancel()

	var persisted int
	m.SetIntervalPersister(func(s int) { persisted = s })

	m.SetScanInterval(30)
	if got := m.ScanIntervalSeconds(); got != 30 {
		t.Errorf("ScanIntervalSeconds = %d, want 30", got)
	}
	if persisted != 30 {
		t.Errorf("persisted = %d, want 30", persisted)
	}

	m.SetScanInterval(0) // clamps up to the 3s default
	if got := m.ScanIntervalSeconds(); got != 3 {
		t.Errorf("clamp low: got %d, want 3", got)
	}
	if persisted != 3 {
		t.Errorf("persisted after clamp = %d, want 3", persisted)
	}

	m.SetScanInterval(100000) // clamps down to 3600
	if got := m.ScanIntervalSeconds(); got != 3600 {
		t.Errorf("clamp high: got %d, want 3600", got)
	}
}

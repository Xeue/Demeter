package hub

import (
	"fmt"
	"testing"

	"github.com/Xeue/Demeter/internal/logging"
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

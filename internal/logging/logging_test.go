package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC) }

// TestEmitsAllButFileGatedByLevel: the GUI emitter receives EVERY record (so the
// front-end can filter), while the text file honours the configured level.
func TestEmitsAllButFileGatedByLevel(t *testing.T) {
	var buf bytes.Buffer
	var emitted []Event
	h := New(&buf, slog.LevelWarn, fixedNow) // file level = Warn
	h.SetEmitter(func(e Event) { emitted = append(emitted, e) })
	log := slog.New(h)

	log.Info("info message") // below the file level
	log.Warn("warn message") // at/above the file level

	// Both are emitted to the GUI.
	if len(emitted) != 2 {
		t.Fatalf("emitted %d events, want 2 (Info + Warn)", len(emitted))
	}
	if emitted[0].Level != "I" || emitted[1].Level != "W" {
		t.Errorf("emitted levels = %q,%q want I,W", emitted[0].Level, emitted[1].Level)
	}

	// Only the Warn line reaches the file.
	out := buf.String()
	if strings.Contains(out, "info message") {
		t.Errorf("file should NOT contain the Info line at Warn level:\n%s", out)
	}
	if !strings.Contains(out, "warn message") {
		t.Errorf("file should contain the Warn line:\n%s", out)
	}
}

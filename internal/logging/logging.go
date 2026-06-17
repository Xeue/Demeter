// Package logging configures slog for Demeter and fans each record to the web UI
// as a structured `log` event (replacing the legacy xeue-logs ANSI round-trip:
// the handler computes level/colour directly, so the front-end doLog does no
// escape-sequence parsing).
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Event is the structured log line sent to the GUI.
type Event struct {
	TimeString string `json:"timeString"`
	Level      string `json:"level"`
	Category   string `json:"category"`
	Message    string `json:"message"`
	Colour     string `json:"colour"`
}

// Emitter receives log events for fan-out to clients (non-blocking).
type Emitter func(Event)

// Handler is a slog.Handler that writes human-readable lines to out and emits
// structured Events to a (later-attached) emitter.
type Handler struct {
	level   slog.Level
	out     io.Writer
	mu      *sync.Mutex
	cat     string
	emitter *atomic.Pointer[Emitter]
	now     func() time.Time
}

// New returns a Handler at the given level writing to out. now supplies the
// timestamp (so tests/headless runs can control it); pass time.Now.
func New(out io.Writer, level slog.Level, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{
		level:   level,
		out:     out,
		mu:      &sync.Mutex{},
		emitter: &atomic.Pointer[Emitter]{},
		now:     now,
	}
}

// SetEmitter attaches (or replaces) the GUI emitter; safe to call after the hub
// is built.
func (h *Handler) SetEmitter(e Emitter) { h.emitter.Store(&e) }

// Enabled accepts every standard record (Debug and up) so the GUI can receive
// ALL logs and filter on the front-end. The text sink (file/stderr) is gated
// separately by the configured level inside Handle.
func (h *Handler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelDebug }

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	cat := h.cat
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "category" || a.Key == "cat" {
			cat = a.Value.String()
			return true
		}
		msg += fmt.Sprintf(" %s=%v", a.Key, a.Value.Any())
		return true
	})
	if cat == "" {
		cat = "SERVER"
	}
	ts := h.now().Format("15:04:05")
	level, colour := levelInfo(r.Level)

	// Text sink honours the configured file level (keeps the log file uncluttered).
	if r.Level >= h.level {
		h.mu.Lock()
		fmt.Fprintf(h.out, "[%s] (%s) %s: %s\n", ts, level, cat, msg)
		h.mu.Unlock()
	}

	// The GUI gets every record; the front-end does the filtering.
	if ep := h.emitter.Load(); ep != nil && *ep != nil {
		(*ep)(Event{TimeString: ts, Level: level, Category: cat, Message: msg, Colour: colour})
	}
	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	for _, a := range attrs {
		if a.Key == "category" || a.Key == "cat" {
			nh.cat = a.Value.String()
		}
	}
	return &nh
}

func (h *Handler) WithGroup(string) slog.Handler { return h }

func levelInfo(l slog.Level) (level, colour string) {
	switch {
	case l >= slog.LevelError:
		return "E", "red"
	case l >= slog.LevelWarn:
		return "W", "yellow"
	case l >= slog.LevelInfo:
		return "I", "green"
	default:
		return "D", "cyan"
	}
}

// LevelFor maps Demeter's logging-level letter (A/D/W/E) to a slog.Level.
func LevelFor(letter string) slog.Level {
	switch letter {
	case "A", "D":
		return slog.LevelDebug
	case "E":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

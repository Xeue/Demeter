package auth

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Record is one audit-log entry for a destructive or auth-related action.
type Record struct {
	TS       time.Time `json:"ts"`
	User     string    `json:"user"`
	Role     Role      `json:"role"`
	Action   string    `json:"action"`
	Target   any       `json:"target,omitempty"`
	ClientIP string    `json:"clientIP,omitempty"`
}

// Audit is an append-only JSONL audit log written on its own goroutine so it can
// never block the request/scan path and is independent of the coalesced state
// writes.
type Audit struct {
	ch   chan Record
	done chan struct{}
	path string
	now  func() time.Time
}

// NewAudit starts an audit writer appending to dir/audit.jsonl.
func NewAudit(dir string, now func() time.Time) *Audit {
	if now == nil {
		now = time.Now
	}
	a := &Audit{
		ch:   make(chan Record, 256),
		done: make(chan struct{}),
		path: filepath.Join(dir, "audit.jsonl"),
		now:  now,
	}
	go a.run()
	return a
}

// Log records an audit entry (non-blocking; drops if the buffer is full).
func (a *Audit) Log(user string, role Role, action string, target any, clientIP string) {
	if a == nil {
		return
	}
	rec := Record{TS: a.now().UTC(), User: user, Role: role, Action: action, Target: target, ClientIP: clientIP}
	select {
	case a.ch <- rec:
	default:
		slog.Warn("audit buffer full, dropping record", "action", action, "user", user)
	}
}

func (a *Audit) run() {
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		slog.Error("audit: mkdir", "err", err)
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Error("audit: open", "err", err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for {
		select {
		case rec := <-a.ch:
			if err := enc.Encode(rec); err != nil {
				slog.Error("audit: write", "err", err)
			}
		case <-a.done:
			// drain
			for {
				select {
				case rec := <-a.ch:
					_ = enc.Encode(rec)
				default:
					return
				}
			}
		}
	}
}

// Close stops the audit writer after draining.
func (a *Audit) Close() {
	if a == nil {
		return
	}
	close(a.done)
}

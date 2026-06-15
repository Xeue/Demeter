package frame

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/device"
)

// countingDialer records dial attempts per IP and can be told to fail.
type countingDialer struct {
	mu       sync.Mutex
	attempts map[string]int
	fail     map[string]bool
}

func newCountingDialer() *countingDialer {
	return &countingDialer{attempts: map[string]int{}, fail: map[string]bool{}}
}

func (d *countingDialer) Dial(_ context.Context, ip string) (device.Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attempts[ip]++
	if d.fail[ip] {
		return nil, errors.New("dial failed")
	}
	return device.NewFakeDevice(), nil
}

func (d *countingDialer) count(ip string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.attempts[ip]
}

// TestConnCacheBackoff: a failed dial suppresses re-dialing within the backoff
// window (so an unreachable card IP is not re-dialed every cycle).
func TestConnCacheBackoff(t *testing.T) {
	d := newCountingDialer()
	d.fail["10.0.0.9"] = true
	c := newConnCache(d)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	c.backoff = 15 * time.Second

	if _, err := c.Device(context.Background(), "10.0.0.9"); err == nil {
		t.Fatal("expected first dial to fail")
	}
	// Second call within the window must NOT dial again.
	if _, err := c.Device(context.Background(), "10.0.0.9"); err == nil {
		t.Fatal("expected backoff to keep returning an error")
	}
	if got := d.count("10.0.0.9"); got != 1 {
		t.Errorf("dial attempts within backoff = %d, want 1", got)
	}
	// After the window elapses, it dials again.
	now = now.Add(20 * time.Second)
	_, _ = c.Device(context.Background(), "10.0.0.9")
	if got := d.count("10.0.0.9"); got != 2 {
		t.Errorf("dial attempts after backoff = %d, want 2", got)
	}
}

// TestConnCachePrune: connections idle beyond maxIdle are closed and dropped,
// while a recently used one is kept.
func TestConnCachePrune(t *testing.T) {
	d := newCountingDialer()
	c := newConnCache(d)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }

	// Dial two IPs.
	if _, err := c.Device(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Device(context.Background(), "live"); err != nil {
		t.Fatal(err)
	}

	// Advance time, then touch only "live".
	now = now.Add(30 * time.Second)
	_, _ = c.Device(context.Background(), "live")

	// Prune anything idle > 20s: "old" goes, "live" stays.
	c.Prune(20 * time.Second)

	c.mu.Lock()
	_, hasOld := c.conns["old"]
	_, hasLive := c.conns["live"]
	c.mu.Unlock()
	if hasOld {
		t.Error("idle connection 'old' should have been pruned")
	}
	if !hasLive {
		t.Error("recently used connection 'live' should have been kept")
	}
}

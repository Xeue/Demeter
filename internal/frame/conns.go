package frame

import (
	"context"
	"sync"
	"time"

	"github.com/Xeue/Demeter/internal/device"
)

// connBackoff is how long a failed dial suppresses re-dialing the same IP, so an
// unreachable card (the normal mid-provisioning state) is not re-dialed every
// poll cycle.
const connBackoff = 15 * time.Second

// connCache holds one persistent device connection per IP for a frame actor
// (the frame IP plus any card IPs reached during a scan), reused across cycles.
// It implements scan.Conns. The scan goroutine reads through it concurrently, so
// it is mutex-guarded; the actor closes all connections on teardown.
//
// A failed dial is remembered for connBackoff so unreachable card IPs fail fast
// instead of re-dialing every cycle, and idle connections (e.g. a card's old IP
// after its IP changed) are pruned by Prune.
type connCache struct {
	dialer  device.Dialer
	backoff time.Duration
	now     func() time.Time

	mu       sync.Mutex
	conns    map[string]device.Device
	lastUsed map[string]time.Time
	lastFail map[string]time.Time
	closed   bool
}

func newConnCache(d device.Dialer) *connCache {
	return &connCache{
		dialer:   d,
		backoff:  connBackoff,
		now:      time.Now,
		conns:    map[string]device.Device{},
		lastUsed: map[string]time.Time{},
		lastFail: map[string]time.Time{},
	}
}

// Device returns a connection for ip, dialing (and caching) one on first use. A
// recent dial failure (within backoff) fails fast without re-dialing.
func (c *connCache) Device(ctx context.Context, ip string) (device.Device, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, device.ErrFrameUnreachable
	}
	if d, ok := c.conns[ip]; ok {
		c.lastUsed[ip] = c.now()
		c.mu.Unlock()
		return d, nil
	}
	if t, ok := c.lastFail[ip]; ok && c.now().Sub(t) < c.backoff {
		c.mu.Unlock()
		return nil, device.ErrFrameUnreachable // still in backoff
	}
	c.mu.Unlock()

	// Dial outside the lock (it can block) then store, guarding against a race
	// where two goroutines dial the same IP at once.
	d, err := c.dialer.Dial(ctx, ip)
	if err != nil {
		c.mu.Lock()
		c.lastFail[ip] = c.now()
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		_ = d.Close()
		return nil, device.ErrFrameUnreachable
	}
	if existing, ok := c.conns[ip]; ok {
		_ = d.Close() // someone beat us to it
		c.lastUsed[ip] = c.now()
		return existing, nil
	}
	c.conns[ip] = d
	c.lastUsed[ip] = c.now()
	delete(c.lastFail, ip)
	return d, nil
}

// Prune closes and drops connections not used within maxIdle (e.g. a card's old
// IP after its IP changed).
func (c *connCache) Prune(maxIdle time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	now := c.now()
	for ip, d := range c.conns {
		if now.Sub(c.lastUsed[ip]) > maxIdle {
			_ = d.Close()
			delete(c.conns, ip)
			delete(c.lastUsed, ip)
		}
	}
}

func (c *connCache) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for ip, d := range c.conns {
		_ = d.Close()
		delete(c.conns, ip)
	}
}

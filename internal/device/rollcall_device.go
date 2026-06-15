package device

import (
	"context"
	"sync"
	"time"

	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/rollcall"
)

// Defaults for the rollcall-backed device.
const (
	defaultPerGetTimeout    = 2 * time.Second
	defaultBatchConcurrency = 16
	defaultDialTimeout      = 3 * time.Second
)

// RollcallDialer opens real RollCall connections (one persistent client per
// frame IP).
type RollcallDialer struct {
	// Port overrides the RollCall TCP port (0 => rollcall.DefaultPort).
	Port int
	// PerGetTimeout bounds a single GET so an absent unit fails fast (0 => default).
	PerGetTimeout time.Duration
	// BatchConcurrency caps simultaneous GETs within one BatchGet (0 => default).
	BatchConcurrency int
	// DialTimeout bounds the TCP connect so an unreachable card (the normal
	// mid-provisioning state) fails fast instead of blocking on the OS timeout
	// (0 => default).
	DialTimeout time.Duration
	// Keepalive enables the (gated) OPEN/IDENTITY session management in SEAM #2.
	Keepalive bool
}

// Dial opens one persistent RollCall connection to frameIP.
func (rd RollcallDialer) Dial(ctx context.Context, frameIP string) (Device, error) {
	var opts []rollcall.Option
	if rd.Port != 0 {
		opts = append(opts, rollcall.WithPort(rd.Port))
	}
	dialCtx := ctx
	if to := orDur(rd.DialTimeout, defaultDialTimeout); to > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, to)
		defer cancel()
	}
	c, err := rollcall.Dial(dialCtx, frameIP, opts...)
	if err != nil {
		return nil, err
	}
	startSession(c, rd.Keepalive)
	return &rollcallDevice{
		c:                c,
		perGetTimeout:    orDur(rd.PerGetTimeout, defaultPerGetTimeout),
		batchConcurrency: orInt(rd.BatchConcurrency, defaultBatchConcurrency),
	}, nil
}

func orDur(v, def time.Duration) time.Duration {
	if v == 0 {
		return def
	}
	return v
}

func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// rollcallDevice is one persistent connection to one frame, multiplexing all
// units behind it — the replacement for spawning rolltrak.exe per command.
type rollcallDevice struct {
	c                *rollcall.Client
	perGetTimeout    time.Duration
	batchConcurrency int
}

func (d *rollcallDevice) Get(ctx context.Context, addr, slot string, cmd uint32) (model.Value, error) {
	unit, err := AddrMapper(addr, slot)
	if err != nil {
		return model.None(), err
	}
	cctx := ctx
	if d.perGetTimeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, d.perGetTimeout)
		defer cancel()
	}
	v, err := d.c.Get(cctx, unit, cmd)
	if err != nil {
		return model.None(), classifyGetErr(ctx, err)
	}
	return fromRollcall(v), nil
}

func (d *rollcallDevice) BatchGet(ctx context.Context, addr, slot string, cmds []uint32) (map[uint32]model.Value, map[uint32]error) {
	values := make(map[uint32]model.Value, len(cmds))
	errs := make(map[uint32]error)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, d.batchConcurrency)

	for _, cmd := range cmds {
		// Stop early if the scan was cancelled.
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(cmd uint32) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				errs[cmd] = ctx.Err()
				mu.Unlock()
				return
			}
			v, err := d.Get(ctx, addr, slot, cmd)
			mu.Lock()
			if err != nil {
				errs[cmd] = err
			} else {
				values[cmd] = v
			}
			mu.Unlock()
		}(cmd)
	}
	wg.Wait()
	return values, errs
}

func (d *rollcallDevice) Set(ctx context.Context, addr, slot string, cmd uint32, v model.Value) (model.Value, error) {
	unit, err := AddrMapper(addr, slot)
	if err != nil {
		return model.None(), err
	}
	echo, err := d.c.Set(ctx, unit, cmd, toRollcall(v))
	if err != nil {
		return model.None(), err
	}
	return fromRollcall(echo), nil
}

func (d *rollcallDevice) Take(ctx context.Context, addr, slot string, takeCmd uint32) error {
	unit, err := AddrMapper(addr, slot)
	if err != nil {
		return err
	}
	return d.c.Take(ctx, unit, takeCmd)
}

func (d *rollcallDevice) Close() error { return d.c.Close() }

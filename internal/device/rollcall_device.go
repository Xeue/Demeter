package device

import (
	"context"
	"strconv"
	"strings"
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

// ParseMode maps a config string to a rollcall transport mode. Anything other
// than "connected" (case-insensitive) is the default, Unconnected (RollTrak
// dialect, port-0 addressing) — the mode Demeter's addressing matches.
func ParseMode(s string) rollcall.Mode {
	if strings.EqualFold(strings.TrimSpace(s), "connected") {
		return rollcall.Connected
	}
	return rollcall.Unconnected
}

// ParseSetOpcode maps a hex string (e.g. "0b", "0d") to the unconnected-mode SET
// opcode, defaulting to the best guess (0x0b) for an empty/invalid value.
func ParseSetOpcode(s string) rollcall.Opcode {
	if v, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(s, "0x")), 16, 8); err == nil && v != 0 {
		return rollcall.Opcode(v)
	}
	return rollcall.OpUReq
}

// RollcallDialer opens real RollCall connections (one persistent client per
// frame IP).
type RollcallDialer struct {
	// Mode selects the RollCall dialect (default Connected — the zero value).
	// app.go sets this from config (default Unconnected).
	Mode rollcall.Mode
	// SetOpcode overrides the unconnected-mode SET opcode (0 => default 0x0b).
	SetOpcode rollcall.Opcode
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
	// Handshake enables the (gated) connect-time IDENTITY login in SEAM #2 — try
	// it when a frame connects (TCP) but never answers RollCall GETs.
	Handshake bool
}

// Dial opens one persistent RollCall connection to frameIP.
func (rd RollcallDialer) Dial(ctx context.Context, frameIP string) (Device, error) {
	opts := []rollcall.Option{rollcall.WithMode(rd.Mode)}
	if rd.SetOpcode != 0 {
		opts = append(opts, rollcall.WithUnconnectedSetOpcode(rd.SetOpcode))
	}
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
	// Unconnected mode logs in itself (0x15) inside rollcall.Dial. The connected
	// IDENTITY handshake only applies to connected mode, and stays gated.
	if rd.Mode == rollcall.Connected {
		startSession(c, rd.Handshake)
	}
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

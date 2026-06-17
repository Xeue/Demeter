package device

import (
	"context"
	"errors"
	"net"

	"github.com/Xeue/Demeter/internal/model"
)

// SEAM #3 — how an offline / unreachable unit surfaces.
//
// The legacy app detected offline cards by textual sentinels in rolltrak's
// stdout. Natively those strings do not exist: an absent unit either errors or
// (worst case) never replies, so a GET would otherwise block to the context
// deadline. The rollcall-backed device wraps each GET in a short per-call
// timeout (see rollcall_device.go) and maps the resulting deadline/closed errors
// to ErrUnitOffline here, so an absent card fails fast instead of stalling a
// 298-parameter batch. This is the place to adapt when the real offline wire
// behaviour (error frame? silence?) is known.

// ErrUnitOffline means a unit (card) did not answer in time / is unreachable.
var ErrUnitOffline = errors.New("device: unit offline/unreachable")

// ErrFrameUnreachable means the frame connection itself could not be opened
// (TCP dial failed: refused, timed out, no route, wrong port).
var ErrFrameUnreachable = errors.New("device: frame unreachable")

// ErrFrameNoResponse means the TCP connection opened but the frame answered no
// GETs — i.e. it is reachable on the network but not replying on the RollCall
// protocol (likely the connect handshake/addressing, not connectivity). Kept
// distinct from ErrFrameUnreachable so the UI/log can point at the right cause.
var ErrFrameNoResponse = errors.New("device: frame not responding")

// legacySentinels are the rolltrak stdout stand-ins the TS code compared against.
// Native reads never produce these, but IsAbsent recognises them so the scan
// port can stay faithful to the original string checks during the transition.
var legacySentinels = map[string]struct{}{
	"StringVal":              {},
	"No rollcall connection": {},
	"Not In Use":             {},
	"No Unit Fitted":         {}, // confirmed in the unconnected capture: an empty/absent slot
}

// IsAbsent reports whether a scanned value means "no usable value": a None
// (native offline / errored GET) or one of the legacy sentinel strings.
func IsAbsent(v model.Value) bool {
	if v.IsNone() {
		return true
	}
	if v.Kind == model.KindStr {
		_, ok := legacySentinels[v.Str]
		return ok
	}
	return false
}

// classifyGetErr maps a GET error to ErrUnitOffline where appropriate. If the
// parent context was cancelled/expired (the scan was aborted or superseded),
// that takes precedence and is returned as-is so callers can distinguish a real
// cancellation from an offline unit.
func classifyGetErr(parent context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, net.ErrClosed) {
		return ErrUnitOffline
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrUnitOffline
	}
	return err
}

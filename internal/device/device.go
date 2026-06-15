// Package device is the seam between Demeter's scan/blast logic and the RollCall
// wire. The scan layer depends only on the Device/Dialer interfaces here and
// speaks in Demeter's addressing terms (hex addr + hex slot tokens, exactly as
// main.ts used them), never in rollcall.Addr. That keeps the rollcall package
// swappable and confines the three not-yet-hardware-confirmed unknowns to one
// place each:
//
//	addr.go    - the CLI cmd@net:addr:slot -> rollcall.Addr packing (SEAM #1)
//	session.go - the OPEN/IDENTITY handshake + keepalive               (SEAM #2)
//	offline.go - how an offline/unreachable unit surfaces              (SEAM #3)
package device

import (
	"context"

	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/rollcall"
)

// Device is one connection to one frame IP. Scan/blast depend only on this.
type Device interface {
	// Get reads one parameter from a unit identified by Demeter's hex addr and
	// slot tokens (e.g. addr "10"/"30", slot "00".."13").
	Get(ctx context.Context, addr, slot string, cmd uint32) (model.Value, error)
	// BatchGet reads many parameters from one (addr,slot). A single failing
	// parameter is reported in the errs map and does not fail the whole batch
	// (mirrors getInfo, which tolerated partial results).
	BatchGet(ctx context.Context, addr, slot string, cmds []uint32) (values map[uint32]model.Value, errs map[uint32]error)
	// Set writes one parameter and returns the device's echoed result value (the
	// RollCall REPLY echoes the resulting value), so the caller can verify the
	// write actually took effect.
	Set(ctx context.Context, addr, slot string, cmd uint32, v model.Value) (model.Value, error)
	// Take fires a commit (a Set of takeCmd to 1).
	Take(ctx context.Context, addr, slot string, takeCmd uint32) error
	// Close releases the underlying connection.
	Close() error
}

// Dialer opens a Device for a frame IP. Lets the manager swap the rollcall-backed
// implementation for a fake in tests.
type Dialer interface {
	Dial(ctx context.Context, frameIP string) (Device, error)
}

// fromRollcall converts a rollcall.Value into a model.Value.
func fromRollcall(v rollcall.Value) model.Value {
	switch v.Kind {
	case rollcall.KindInt:
		return model.IntVal(int64(v.Int))
	case rollcall.KindString:
		return model.StrVal(v.Str)
	default:
		return model.None()
	}
}

// toRollcall converts a model.Value into a rollcall.Value for a SET.
func toRollcall(v model.Value) rollcall.Value {
	switch v.Kind {
	case model.KindInt:
		return rollcall.Int(uint32(v.Int))
	case model.KindStr:
		return rollcall.Str(v.Str)
	default:
		return rollcall.Str("")
	}
}

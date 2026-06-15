package device

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Xeue/Demeter/internal/model"
)

// FakeDevice is an in-memory Device for tests: it answers GETs from a seeded
// map, records SETs/Takes, can mark units offline, and tracks peak concurrent
// GETs (to assert the worker-pool bound). Safe for concurrent use.
type FakeDevice struct {
	mu      sync.Mutex
	values  map[string]map[uint32]model.Value
	offline map[string]bool
	reject  map[string]map[uint32]bool // (addr,slot,cmd) that won't apply a Set (echoes old value)
	sets    []FakeSet
	takes   []FakeTake
	closed  bool

	// GetDelay, if set, makes each Get block this long (respecting ctx) — used to
	// exercise concurrency/cancellation.
	GetDelay time.Duration

	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

// FakeSet records a Set call.
type FakeSet struct {
	Addr, Slot string
	Cmd        uint32
	Value      model.Value
}

// FakeTake records a Take call.
type FakeTake struct {
	Addr, Slot string
	Cmd        uint32
}

// NewFakeDevice returns an empty FakeDevice.
func NewFakeDevice() *FakeDevice {
	return &FakeDevice{
		values:  map[string]map[uint32]model.Value{},
		offline: map[string]bool{},
	}
}

func fakeKey(addr, slot string) string { return addr + "/" + slot }

// Seed sets the value a GET of (addr,slot,cmd) will return.
func (f *FakeDevice) Seed(addr, slot string, cmd uint32, v model.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := fakeKey(addr, slot)
	if f.values[k] == nil {
		f.values[k] = map[uint32]model.Value{}
	}
	f.values[k][cmd] = v
}

// SetOffline marks (addr,slot) as unreachable; GETs to it return ErrUnitOffline.
func (f *FakeDevice) SetOffline(addr, slot string, off bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline[fakeKey(addr, slot)] = off
}

// RejectSet makes (addr,slot,cmd) refuse to apply a Set: the stored value is
// left unchanged and the Set echoes the old value, simulating a device that
// rejects a write (used to test verify-and-retry).
func (f *FakeDevice) RejectSet(addr, slot string, cmd uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := fakeKey(addr, slot)
	if f.reject[k] == nil {
		if f.reject == nil {
			f.reject = map[string]map[uint32]bool{}
		}
		f.reject[k] = map[uint32]bool{}
	}
	f.reject[k][cmd] = true
}

// Sets returns a copy of the recorded SET calls.
func (f *FakeDevice) Sets() []FakeSet {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeSet(nil), f.sets...)
}

// Takes returns a copy of the recorded Take calls.
func (f *FakeDevice) Takes() []FakeTake {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeTake(nil), f.takes...)
}

// MaxInFlight returns the peak number of concurrent GETs observed.
func (f *FakeDevice) MaxInFlight() int { return int(f.maxInFlight.Load()) }

func (f *FakeDevice) Get(ctx context.Context, addr, slot string, cmd uint32) (model.Value, error) {
	n := f.inFlight.Add(1)
	for {
		m := f.maxInFlight.Load()
		if n <= m || f.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	defer f.inFlight.Add(-1)

	if f.GetDelay > 0 {
		select {
		case <-time.After(f.GetDelay):
		case <-ctx.Done():
			return model.None(), ctx.Err()
		}
	}
	if ctx.Err() != nil {
		return model.None(), ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	k := fakeKey(addr, slot)
	if f.offline[k] {
		return model.None(), ErrUnitOffline
	}
	if vals, ok := f.values[k]; ok {
		if v, ok := vals[cmd]; ok {
			return v, nil
		}
	}
	return model.None(), ErrUnitOffline
}

func (f *FakeDevice) BatchGet(ctx context.Context, addr, slot string, cmds []uint32) (map[uint32]model.Value, map[uint32]error) {
	values := make(map[uint32]model.Value, len(cmds))
	errs := make(map[uint32]error)
	for _, cmd := range cmds {
		if ctx.Err() != nil {
			errs[cmd] = ctx.Err()
			continue
		}
		v, err := f.Get(ctx, addr, slot, cmd)
		if err != nil {
			errs[cmd] = err
		} else {
			values[cmd] = v
		}
	}
	return values, errs
}

func (f *FakeDevice) Set(ctx context.Context, addr, slot string, cmd uint32, v model.Value) (model.Value, error) {
	if ctx.Err() != nil {
		return model.None(), ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets = append(f.sets, FakeSet{Addr: addr, Slot: slot, Cmd: cmd, Value: v})
	k := fakeKey(addr, slot)
	if f.reject[k][cmd] {
		// Refuse to apply: echo the existing (unchanged) value, if any.
		if vals, ok := f.values[k]; ok {
			if old, ok := vals[cmd]; ok {
				return old, nil
			}
		}
		return model.None(), nil
	}
	// Reflect the write so a subsequent scan sees the new active value, and echo it.
	if f.values[k] == nil {
		f.values[k] = map[uint32]model.Value{}
	}
	f.values[k][cmd] = v
	return v, nil
}

func (f *FakeDevice) Take(ctx context.Context, addr, slot string, takeCmd uint32) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.takes = append(f.takes, FakeTake{Addr: addr, Slot: slot, Cmd: takeCmd})
	return nil
}

func (f *FakeDevice) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// FakeDialer hands out FakeDevices per frame IP (creating one on first dial),
// or a seeded dial error.
type FakeDialer struct {
	mu      sync.Mutex
	Devices map[string]*FakeDevice
	Err     map[string]error
}

// NewFakeDialer returns an empty FakeDialer.
func NewFakeDialer() *FakeDialer {
	return &FakeDialer{Devices: map[string]*FakeDevice{}, Err: map[string]error{}}
}

// Dial returns the FakeDevice for ip (creating one if absent) or a seeded error.
func (fd *FakeDialer) Dial(ctx context.Context, ip string) (Device, error) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	if e := fd.Err[ip]; e != nil {
		return nil, e
	}
	d := fd.Devices[ip]
	if d == nil {
		d = NewFakeDevice()
		fd.Devices[ip] = d
	}
	return d, nil
}

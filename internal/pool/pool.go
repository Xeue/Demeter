// Package pool provides a bounded, context-aware semaphore that caps the total
// number of concurrent RollCall operations across all frames — the real version
// of the legacy `jobs` counter (main.ts), which was only logged, never enforced.
package pool

import "context"

// Pool bounds concurrency to a fixed number of slots.
type Pool struct {
	sem chan struct{}
}

// New returns a Pool with n slots (minimum 1).
func New(n int) *Pool {
	if n < 1 {
		n = 1
	}
	return &Pool{sem: make(chan struct{}, n)}
}

// Acquire takes a slot, blocking until one is free or ctx is done.
func (p *Pool) Acquire(ctx context.Context) error {
	select {
	case p.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot.
func (p *Pool) Release() { <-p.sem }

// Cap returns the configured slot count.
func (p *Pool) Cap() int { return cap(p.sem) }

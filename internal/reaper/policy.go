// Package reaper moves messages that have spent their attempts to their
// queue's dead letter partition.
//
// It is not what makes the system correct. Claim is given the queue's attempt
// ceiling, so a message at it is already undeliverable; a sweep that never
// runs delays a message's arrival in the dead letter partition and nothing
// else. What the reaper buys is timeliness.
//
// It reaches storage through the SPI alone and holds no queue state of its
// own, which is what would let it be lifted into a separate binary later
// without touching the engine.
package reaper

import (
	"math/rand/v2"
	"time"
)

// Defaults for how often a queue is swept and how much one sweep may move.
const (
	DefaultInterval    = 1 * time.Second
	DefaultIntervalMax = 30 * time.Second
	DefaultScanLimit   = 100
)

// Policy paces one queue's sweep.
//
// Ten thousand queues sweeping every second is ten thousand requests per
// second against a storage server we did not write, almost all of which find
// nothing. Jitter spreads that load off a single tick; backoff removes most of
// it. A queue that finds work snaps back to the floor, so busy queues stay
// responsive and idle ones go quiet.
type Policy struct {
	floor   time.Duration
	ceiling time.Duration

	// current is the interval the next wait is drawn from. It starts at the
	// floor and doubles on every empty scan, up to the ceiling.
	current time.Duration

	// Jitter returns a fraction in [0, 1). It is a field so that a test can
	// make the schedule deterministic.
	Jitter func() float64
}

// NewPolicy returns a policy that starts at floor and backs off to ceiling.
func NewPolicy(floor, ceiling time.Duration) *Policy {
	if floor <= 0 {
		floor = DefaultInterval
	}
	if ceiling < floor {
		ceiling = floor
	}
	return &Policy{
		floor:   floor,
		ceiling: ceiling,
		current: floor,
		Jitter:  rand.Float64,
	}
}

// InitialDelay is how long a sweep waits before its first scan. It is a
// randomised offset within the floor, so that sweeps started together — every
// queue at engine startup, for instance — do not all fire on the same tick.
func (p *Policy) InitialDelay() time.Duration {
	return time.Duration(p.Jitter() * float64(p.floor))
}

// Next reports how long to wait after a scan, and moves the interval.
//
// found is whether the scan returned anything. Anything at all resets the
// interval to the floor: a queue that has work probably has more.
func (p *Policy) Next(found bool) time.Duration {
	if found {
		p.current = p.floor
	} else if p.current < p.ceiling {
		p.current *= 2
		if p.current > p.ceiling {
			p.current = p.ceiling
		}
	}
	return p.wait()
}

// wait is the current interval with jitter applied to its phase: a draw from
// the second half of the interval, so that a backed-off sweep never becomes
// more frequent than the interval it backed off to by more than half.
func (p *Policy) wait() time.Duration {
	half := float64(p.current) / 2
	return time.Duration(half + p.Jitter()*half)
}

// Interval is the current unjittered interval, before jitter is applied to
// its phase.
func (p *Policy) Interval() time.Duration { return p.current }

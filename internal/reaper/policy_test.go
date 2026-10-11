package reaper_test

import (
	"testing"
	"time"

	"github.com/koppathachan/kyu/internal/reaper"
)

func TestPolicyBacksOffAndSnapsBack(t *testing.T) {
	p := reaper.NewPolicy(time.Second, 8*time.Second)
	p.Jitter = func() float64 { return 0 } // a deterministic schedule

	if got := p.Interval(); got != time.Second {
		t.Fatalf("a sweep starts at %s, want the floor", got)
	}

	for _, want := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second} {
		p.Next(false)
		if got := p.Interval(); got != want {
			t.Fatalf("after an empty scan the interval is %s, want %s", got, want)
		}
	}

	p.Next(true)
	if got := p.Interval(); got != time.Second {
		t.Fatalf("after a scan that found work the interval is %s, want the floor", got)
	}
}

func TestPolicyJittersItsPhase(t *testing.T) {
	p := reaper.NewPolicy(time.Second, time.Minute)

	p.Jitter = func() float64 { return 0 }
	if got := p.InitialDelay(); got != 0 {
		t.Fatalf("the earliest initial delay is %s, want 0", got)
	}
	p.Jitter = func() float64 { return 0.999 }
	if got := p.InitialDelay(); got <= 900*time.Millisecond || got >= time.Second {
		t.Fatalf("the latest initial delay is %s, want it just inside the floor", got)
	}

	// Two sweeps started together do not fire together.
	a, b := reaper.NewPolicy(time.Second, time.Minute), reaper.NewPolicy(time.Second, time.Minute)
	same := 0
	for range 20 {
		if a.InitialDelay() == b.InitialDelay() {
			same++
		}
	}
	if same > 1 {
		t.Fatalf("%d of 20 offsets matched, so the phase is not jittered", same)
	}
}

// A ceiling below the floor is not a schedule that shrinks; it is clamped.
func TestPolicyClampsAnInvertedRange(t *testing.T) {
	p := reaper.NewPolicy(10*time.Second, time.Second)
	p.Jitter = func() float64 { return 0 }

	p.Next(false)
	if got := p.Interval(); got != 10*time.Second {
		t.Fatalf("the interval became %s, want it held at the floor", got)
	}
}

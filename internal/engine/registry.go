package engine

import (
	"context"
	"sync"
	"time"

	"github.com/koppathachan/kyu/internal/queue"
)

// registry is what this engine knows about queues, and what it is doing about
// them.
//
// Four facts travel together and are always decided together: which queues can
// be served and on what settings, which exist but cannot be read, which were
// absent recently enough to still say so, and which have a sweep running. They
// are one type because every transition touches more than one of them —
// adopting a queue clears its absence and its unreadability and starts its
// sweep; finding it unreadable stops the sweep and withdraws it from service.
// Split across four fields of Server, those transitions were four statements
// that had to be remembered in the right order.
type registry struct {
	// startSweep begins sweeping a queue and returns the stop for it. It is
	// injected because starting one is the engine's business — it owns the
	// goroutine and the lifetime — while knowing whether one should be
	// running is the registry's.
	startSweep func(name string, settings queue.Settings) context.CancelFunc

	mu sync.Mutex
	// served holds the settings of every queue this engine can serve.
	served map[string]queue.Settings
	// unreadable holds queues that exist but whose settings this engine
	// cannot decode. They are refused rather than guessed at.
	unreadable map[string]struct{}
	// absent remembers, briefly, that a name was not in storage, so that a
	// client looping on a misspelling does not turn every call into an
	// enumeration. Each entry is the instant it expires.
	absent map[string]time.Time
	// sweeps holds the stop function of each running sweep, by queue.
	sweeps map[string]context.CancelFunc
}

// standing is what the registry knows about one name.
type standing int

const (
	// unknown: nothing is on record. The caller must look in storage before
	// concluding anything — a queue created a moment ago on another replica
	// is real, and this engine simply has not met it.
	unknown standing = iota
	// served: the queue's settings are known and it can be served.
	served
	// unreadable: the queue exists and this engine cannot read its settings.
	unreadable
	// absent: storage was asked recently and did not have it.
	absent
)

func newRegistry(startSweep func(name string, settings queue.Settings) context.CancelFunc) *registry {
	return &registry{
		startSweep: startSweep,
		served:     make(map[string]queue.Settings),
		unreadable: make(map[string]struct{}),
		absent:     make(map[string]time.Time),
		sweeps:     make(map[string]context.CancelFunc),
	}
}

// lookup reports what is on record for a name, deciding an absence's expiry
// against the instant given — the engine's clock, as everything else is.
func (r *registry) lookup(name string, now time.Time) (queue.Settings, standing) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if settings, ok := r.served[name]; ok {
		return settings, served
	}
	if _, bad := r.unreadable[name]; bad {
		return queue.Settings{}, unreadable
	}
	if expiry, ok := r.absent[name]; ok && now.Before(expiry) {
		return queue.Settings{}, absent
	}
	return queue.Settings{}, unknown
}

// adopt records a queue's settings and makes sure it is being swept.
func (r *registry) adopt(name string, settings queue.Settings) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.served[name] = settings
	delete(r.unreadable, name)
	// An absence contradicted by this engine's own creation is not a race,
	// and waiting out the TTL would mean refusing a client a queue the engine
	// it is talking to has just made.
	delete(r.absent, name)
	r.ensureSweepLocked(name, settings)
}

// markUnreadable withdraws a queue from service and stops its sweep. It
// reports whether this is new, so that the caller names the queue once rather
// than on every enumeration.
func (r *registry) markUnreadable(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, already := r.unreadable[name]
	r.unreadable[name] = struct{}{}
	delete(r.served, name)
	r.stopSweepLocked(name)
	return !already
}

// rememberAbsent records that storage did not have this name, until the given
// instant.
//
// The window is bounded and short on purpose: an absence remembered here can
// be contradicted by a creation there, which is in miniature the same failure
// that looking a queue up in storage exists to prevent.
func (r *registry) rememberAbsent(name string, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.absent[name] = until
}

// ensureSweepLocked starts a sweep if one is not already running. It is called
// with r.mu held.
func (r *registry) ensureSweepLocked(name string, settings queue.Settings) {
	if queue.IsDLQ(name) {
		return // there is nowhere for a message to move on from one
	}
	if _, running := r.sweeps[name]; running {
		return
	}
	if stop := r.startSweep(name, settings); stop != nil {
		r.sweeps[name] = stop
	}
}

// stopSweepLocked ends a queue's sweep. It is called with r.mu held.
func (r *registry) stopSweepLocked(name string) {
	if stop, running := r.sweeps[name]; running {
		stop()
		delete(r.sweeps, name)
	}
}

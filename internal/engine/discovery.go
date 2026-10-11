package engine

import (
	"context"
	"time"

	"github.com/koppathachan/kyu/internal/queue"
)

// Start begins the engine's background work: one sweep per queue, and the
// periodic re-enumeration that finds queues created elsewhere.
//
// Startup enumeration alone is not enough, and its absence fails in the
// direction that matters. A queue created on one replica is unknown to every
// replica started before it; those run no sweep for it, and if the replica
// that created it goes away, its messages sit at the attempt ceiling —
// undeliverable and untransferred — for as long as that lasts.
//
// A failure to enumerate at startup is returned. The engine can still serve:
// a client naming a queue triggers a lookup of its own.
func (s *Server) Start(ctx context.Context) error {
	err := s.discover(ctx)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.discoveryLoop()
	}()

	return err
}

// discoveryLoop re-enumerates partitions on a timer for as long as the engine
// runs. One call per interval per replica, against a per-queue scan cost that
// already dwarfs it.
func (s *Server) discoveryLoop() {
	ticker := time.NewTicker(s.cfg.DiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.lifetime.Done():
			return
		case <-ticker.C:
			if err := s.discover(s.lifetime); err != nil && s.lifetime.Err() == nil {
				s.log.Error("partitions could not be enumerated, so queues created elsewhere are not swept here yet",
					"error", err)
			}
		}
	}
}

// settingsFor returns a queue's policy, looking it up in storage if this
// engine has not seen it.
//
// A queue this engine has never heard of is not absent. Queues are created on
// whichever replica a client happened to reach, so one created a moment ago
// elsewhere is real, is durable, and is simply not known here yet.
func (s *Server) settingsFor(ctx context.Context, name string) (queue.Settings, error) {
	switch settings, standing := s.registry.lookup(name, s.now()); standing {
	case served:
		return settings, nil
	case unreadable:
		return queue.Settings{}, errUnreadableQueue(name)
	case absent:
		return queue.Settings{}, errNoSuchQueue(name)
	}

	if err := s.discover(ctx); err != nil {
		return queue.Settings{}, err
	}

	switch settings, standing := s.registry.lookup(name, s.now()); standing {
	case served:
		return settings, nil
	case unreadable:
		// Discovery finds a queue; it does not exempt one.
		return queue.Settings{}, errUnreadableQueue(name)
	}

	s.registry.rememberAbsent(name, s.now().Add(s.cfg.NegativeCacheTTL))
	return queue.Settings{}, errNoSuchQueue(name)
}

// discover enumerates partitions and reconciles what this engine knows with
// what storage holds.
//
// It is what makes a queue created on another replica servable here, and what
// starts a sweep for a queue this replica never created. Both matter: without
// the first a client is refused a queue that exists, and without the second a
// queue whose creating replica has gone away is swept by nobody.
func (s *Server) discover(ctx context.Context) error {
	s.discoverMu.Lock()
	defer s.discoverMu.Unlock()

	partitions, err := s.storage.ListPartitions(ctx)
	if err != nil {
		return err
	}

	for _, p := range partitions {
		name := p.GetName()
		// A dead letter partition is the end of the line. It is not a queue a
		// client may name, and there is nowhere for a message to move on to,
		// so it is neither adopted nor swept.
		if queue.IsDLQ(name) {
			continue
		}

		settings, err := queue.DecodeSettings(p.GetSettings())
		if err != nil {
			s.markUnreadable(name, err)
			continue
		}
		s.registry.adopt(name, settings)
	}
	return nil
}

// markUnreadable refuses to serve a queue whose settings this engine cannot
// decode, and keeps serving every other queue.
//
// This is not the same judgement as an ordering violation, which is reported
// while service continues. There the engine knows the queue's policy and can
// still honour it; here it does not know the attempt ceiling, so claiming
// would risk delivering past a limit it cannot see. Refusing is forced rather
// than chosen.
func (s *Server) markUnreadable(name string, cause error) {
	if s.registry.markUnreadable(name) {
		s.log.Error("a queue's settings cannot be read by this engine, so it will not be served or swept",
			"queue", name, "backend", s.cfg.StorageTarget, "cause", cause)
	}
	s.positions.Forget(name)
}

// startSweep begins sweeping one queue, and returns the stop for it. It is
// what the registry calls when a queue becomes servable; it returns nil when
// this engine is not sweeping at all, or is shutting down.
func (s *Server) startSweep(name string, settings queue.Settings) context.CancelFunc {
	if !s.sweeping {
		return nil
	}
	if s.lifetime.Err() != nil {
		return nil // the engine is stopping
	}

	ctx, cancel := context.WithCancel(s.lifetime)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sweeper.Sweep(ctx, name, settings.AttemptCeiling)
	}()
	return cancel
}

package reaper

import (
	"context"
	"log/slog"
	"time"

	"github.com/koppathachan/kyu/internal/queue"
	"github.com/koppathachan/kyu/internal/spi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Config is how a deployment paces every sweep it runs.
type Config struct {
	// Interval is the floor a sweep backs off from.
	Interval time.Duration
	// IntervalMax is the ceiling a sweep backs off to.
	IntervalMax time.Duration
	// ScanLimit caps each sweep's Scan. One is always supplied, so storage is
	// never asked to choose the reaper's throughput.
	ScanLimit uint32
}

// DefaultConfig is the pacing a deployment gets without saying anything.
func DefaultConfig() Config {
	return Config{
		Interval:    DefaultInterval,
		IntervalMax: DefaultIntervalMax,
		ScanLimit:   DefaultScanLimit,
	}
}

// Sweeper transfers exhausted messages, one queue at a time.
//
// It holds no queue state: a caller that knows a queue's name and its attempt
// ceiling has told it everything it needs. Those two facts come from the
// engine, which read them from the queue's settings.
type Sweeper struct {
	storage *spi.Client
	now     func() time.Time
	log     *slog.Logger
	cfg     Config
}

// New returns a sweeper that reaches storage through the given client and
// decides eligibility against the given clock — the engine's, which is the
// only clock in the system.
func New(storage *spi.Client, now func() time.Time, log *slog.Logger, cfg Config) *Sweeper {
	if cfg.ScanLimit == 0 {
		cfg.ScanLimit = DefaultScanLimit
	}
	return &Sweeper{storage: storage, now: now, log: log, cfg: cfg}
}

// Sweep moves this queue's exhausted messages for as long as ctx lives.
//
// It begins at a randomised offset and backs off while it finds nothing, so
// that ten thousand idle queues do not become ten thousand requests a second
// against a storage server we did not write.
func (s *Sweeper) Sweep(ctx context.Context, name string, ceiling uint32) {
	policy := NewPolicy(s.cfg.Interval, s.cfg.IntervalMax)

	timer := time.NewTimer(policy.InitialDelay())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		found := s.SweepOnce(ctx, name, ceiling)
		timer.Reset(policy.Next(found))
	}
}

// SweepOnce scans for messages whose hold has lapsed with their attempts
// spent, and moves each one. It reports whether the scan found anything, which
// is what the backoff is driven by.
func (s *Sweeper) SweepOnce(ctx context.Context, name string, ceiling uint32) bool {
	items, err := s.storage.Scan(ctx, name, s.now().UnixNano(), ceiling, s.cfg.ScanLimit)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("a sweep could not scan", "queue", name, "error", err)
		}
		return false
	}
	if len(items) == 0 {
		return false
	}

	for _, it := range items {
		if ctx.Err() != nil {
			return true
		}
		s.transfer(ctx, name, it.GetId())
	}
	// A scan that found anything resets the interval: a queue with work
	// probably has more, and a backlog is cleared over successive sweeps.
	return true
}

// transfer moves one exhausted message to the queue's dead letter partition.
//
// No Delete is issued. The message keeps its id, its payload and its attempt
// count — the count is the record of how hard the queue tried, and the only
// evidence of that left once the message has left the queue.
func (s *Sweeper) transfer(ctx context.Context, name, id string) {
	dlq := queue.DLQName(name)

	err := s.storage.Move(ctx, name, dlq, id)
	if err == nil {
		return
	}

	switch status.Code(err) {
	case codes.NotFound:
		// Another replica's sweep moved it first, or a consumer settled it
		// between the scan and the move. Ordinary: the condition will not
		// change, so there is nothing to retry and nothing to report.
	case codes.AlreadyExists:
		// Not ordinary. An id is preserved across a move, so a destination
		// already holding it means two messages share an id, which no correct
		// sequence of engine calls produces.
		s.log.Error("two messages share an id, so one could not be moved to the dead letter partition",
			"source", name, "destination", dlq, "id", id, "error", err)
	default:
		if ctx.Err() == nil {
			s.log.Error("a message could not be moved to the dead letter partition",
				"source", name, "destination", dlq, "id", id, "error", err)
		}
	}
}

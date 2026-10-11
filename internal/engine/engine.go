// Package engine implements kyu's queue semantics.
//
// Every queue concept lives here or in internal/queue: ordering, holds,
// attempt counting, dead letter transfer. The engine reaches durable state
// through kyu.storage.v1.Storage and through nothing else — no connection,
// handle or configuration belonging to a storage implementation, and no branch
// anywhere on which backend it is talking to.
//
// It is also the only clock in the system. Every SPI call whose outcome
// depends on time is given the instant to decide against; storage never reads
// a clock of its own, so the two can never disagree about whether a hold has
// lapsed.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/koppathachan/kyu/internal/queue"
	"github.com/koppathachan/kyu/internal/reaper"
	"github.com/koppathachan/kyu/internal/spi"
	kyuv1 "github.com/koppathachan/kyu/kyu/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server serves kyu.v1.Kyu over one storage backend.
type Server struct {
	kyuv1.UnimplementedKyuServer

	cfg     Config
	storage *spi.Client
	sweeper *reaper.Sweeper
	log     *slog.Logger

	// now is the engine's clock, and the only one in the system. It is a
	// field so that a test can drive time rather than sleep through it.
	now func() time.Time

	// positions detects a backend assigning positions out of order. It is
	// diagnostic: in-memory, best-effort, and lost on restart by design.
	positions *queue.PositionWatch

	// registry is what this engine knows about queues and what it is doing
	// about them: which it serves, which it cannot read, which were absent
	// recently, and which have a sweep running.
	registry *registry

	// sweeping is whether the reaper runs at all. See WithSweeping.
	sweeping bool

	// dialOptions are the gRPC options used to reach storage. They exist so
	// that a test can dial a server on a pipe.
	dialOptions []grpc.DialOption

	// discoverMu serialises enumeration, so that the reaper's timer and a
	// client's lookup do not enumerate at the same moment.
	discoverMu sync.Mutex

	// lifetime is cancelled by Close, ending every sweep and the discovery
	// loop with it.
	lifetime context.Context
	stop     context.CancelFunc
	wg       sync.WaitGroup
}

// Option adjusts a Server. The options exist for tests and for a deployment
// with an unusual arrangement; ordinary configuration goes through Config.
type Option func(*Server)

// WithClock replaces the engine's clock.
func WithClock(now func() time.Time) Option { return func(s *Server) { s.now = now } }

// WithLogger replaces the engine's logger.
func WithLogger(l *slog.Logger) Option { return func(s *Server) { s.log = l } }

// WithSweeping turns the reaper on or off. It is on by default.
//
// Turning it off is safe: Claim is given the queue's ceiling, so a message
// that has spent its attempts is undeliverable whether or not anything is
// sweeping. What stops is the transfer to the dead letter partition, which is
// a matter of timeliness alone. It exists for tests that assert exactly that,
// and for a deployment that runs sweeping somewhere else.
func WithSweeping(on bool) Option { return func(s *Server) { s.sweeping = on } }

// WithDialOptions sets the gRPC options used to reach storage.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(s *Server) { s.dialOptions = opts }
}

// New connects an engine to the storage server a deployment supplies.
//
// The connection is lazy: nothing is called on the backend until a client asks
// for something or Start enumerates partitions.
func New(cfg Config, opts ...Option) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	s := &Server{
		cfg:      cfg,
		log:      slog.Default(),
		now:      time.Now,
		sweeping: true,
	}
	for _, opt := range opts {
		opt(s)
	}

	client, err := spi.Dial(cfg.StorageTarget, s.dialOptions...)
	if err != nil {
		return nil, err
	}
	s.storage = client
	s.sweeper = reaper.New(client, s.now, s.log, cfg.sweeperConfig())
	s.positions = queue.NewPositionWatch(cfg.StorageTarget)
	s.lifetime, s.stop = context.WithCancel(context.Background())
	s.registry = newRegistry(s.startSweep)
	return s, nil
}

// Close stops every sweep and releases the storage connection.
func (s *Server) Close() error {
	s.stop()
	s.wg.Wait()
	return s.storage.Close()
}

// CreateQueue creates a queue and its dead letter companion.
func (s *Server) CreateQueue(ctx context.Context, req *kyuv1.CreateQueueRequest) (*kyuv1.CreateQueueResponse, error) {
	name := req.GetQueue()
	if err := queue.CheckName(name); err != nil {
		return nil, errInvalidName(err)
	}

	settings := queue.Settings{AttemptCeiling: req.GetAttemptCeiling()}
	if req.GetHoldDuration() != nil {
		settings.HoldDuration = req.GetHoldDuration().AsDuration()
	}
	// The defaults are recorded rather than assumed: what ListPartitions
	// returns is the whole of a queue's policy, for this engine and for every
	// other one that meets it.
	settings = settings.WithDefaults()
	encoded := settings.Encode()

	// The companion is created first, and its already existing is not a
	// failure. The queue's own partition is what defines whether the queue
	// exists, so creating the companion first means a creation that dies
	// half-way leaves nothing claiming to be a queue, and a retry completes
	// rather than finding itself permanently refused by its own leftovers.
	if err := s.storage.CreatePartition(ctx, queue.DLQName(name), encoded); err != nil {
		if status.Code(err) != codes.AlreadyExists {
			return nil, fmt.Errorf("the dead letter companion could not be created, so the queue was not: %w", err)
		}
	}
	if err := s.storage.CreatePartition(ctx, name, encoded); err != nil {
		return nil, err
	}

	s.registry.adopt(name, settings)
	return &kyuv1.CreateQueueResponse{}, nil
}

// Enqueue appends one message to a queue.
func (s *Server) Enqueue(ctx context.Context, req *kyuv1.EnqueueRequest) (*kyuv1.EnqueueResponse, error) {
	name := req.GetQueue()
	if err := queue.CheckName(name); err != nil {
		return nil, errInvalidName(err)
	}
	if _, err := s.settingsFor(ctx, name); err != nil {
		return nil, err
	}

	// The id is the engine's. It names the message for acknowledgement and is
	// stable across a move; the position is storage's, orders the message
	// within one partition, and is reassigned when it moves.
	id := newMessageID()

	// The ordering check is taken around the Put, comparing only against
	// positions whose calls had already completed when this one was issued.
	before := s.positions.Begin(name)
	position, err := s.storage.Put(ctx, name, id, req.GetPayload())
	if err != nil {
		return nil, err
	}
	if v := s.positions.Complete(name, before, position); v != nil {
		// Reported, and the queue keeps being served. Taking it out of
		// service would convert an ordering fault we cannot fix into an
		// outage we caused.
		s.log.Error("storage assigned a position that went backwards, so this queue is no longer in order",
			"backend", v.Backend, "partition", v.Partition,
			"earlier_completed_position", v.Earlier, "returned_position", v.Returned)
	}

	return &kyuv1.EnqueueResponse{Id: id, Position: position}, nil
}

// newMessageID returns an id the engine assigns to a message.
//
// It reports no error: crypto/rand.Read fills the slice or panics, and a
// process that cannot read randomness has no business serving a queue. There
// is deliberately no status for this — the error table names statuses for
// outcomes, and this is not one of them.
func newMessageID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

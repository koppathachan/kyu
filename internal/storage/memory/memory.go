// Package memory is a reference implementation of kyu.storage.v1.Storage.
//
// It is a reference and a test fixture. It is not a production backend and
// must not be deployed as one: it holds everything in process memory, so a
// restart loses every message, and it is a single process, so it offers no
// durability, no replication and no bound on what it will hold.
//
// What it is for is to say, in code, what the contract's comments say in
// prose, and to give kyu's own tests a backend to run against over real gRPC.
// A deployment supplies its own implementation of the same service.
//
// Two rules of the contract are visible in the shape of this file.
//
// Nothing here reads a clock. Every comparison against time is between a
// number the caller supplied and a number the caller supplied earlier; this
// package imports no clock and has no way to consult one.
//
// Delete is the only operation that removes an item. No other path deletes,
// evicts, expires or trims anything, on any bound.
package memory

import (
	"context"
	"sync"

	storagev1 "github.com/koppathachan/kyu/kyu/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// item is one stored value and its bookkeeping.
//
// holdUntil is a nillable number, not a time: nil means the item is not held,
// and a value means only that the caller once handed this number over. This
// server never compares it against anything but another number the caller
// gives it.
type item struct {
	id        string
	position  int64
	value     []byte
	holdUntil *int64
	attempts  uint32
}

// partition holds items in position order, under one mutex.
//
// The single mutex is what makes Claim's indivisibility structural: selection,
// hold and increment all happen inside one critical section because there is
// only one lock and it is held across the whole operation, not because each
// step was written carefully.
type partition struct {
	name     string
	settings []byte

	mu sync.Mutex
	// next is the position counter. It is read and incremented inside the
	// write's critical section, never derived from a clock — a position taken
	// from a clock reorders items under skew, rarely and without any error to
	// show for it.
	next int64
	// items is ordered by position, ascending.
	items []*item
	// byID indexes items for the by-id operations. It holds the same pointers
	// as items and is kept in step with it.
	byID map[string]*item
}

// Server is an in-memory kyu.storage.v1.Storage.
type Server struct {
	storagev1.UnimplementedStorageServer

	// mu guards the partition map alone. It is never held while a partition's
	// own mutex is held, so the two cannot deadlock against each other.
	mu         sync.RWMutex
	partitions map[string]*partition
}

// New returns an empty reference server.
func New() *Server {
	return &Server{partitions: make(map[string]*partition)}
}

// lookup finds a partition, releasing the map lock before the caller takes the
// partition's own.
func (s *Server) lookup(name string) (*partition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.partitions[name]
	return p, ok
}

func notFoundPartition(name string) error {
	return status.Errorf(codes.NotFound, "partition %q does not exist", name)
}

func (s *Server) CreatePartition(_ context.Context, req *storagev1.CreatePartitionRequest) (*storagev1.CreatePartitionResponse, error) {
	p := req.GetPartition()
	if p.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "partition name is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.partitions[p.GetName()]; ok {
		return nil, status.Errorf(codes.AlreadyExists, "partition %q already exists", p.GetName())
	}
	// The settings value is stored as given and returned as given. This server
	// does not read it, and nothing below branches on its contents.
	s.partitions[p.GetName()] = &partition{
		name:     p.GetName(),
		settings: append([]byte(nil), p.GetSettings()...),
		next:     1,
		byID:     make(map[string]*item),
	}
	return &storagev1.CreatePartitionResponse{}, nil
}

func (s *Server) ListPartitions(context.Context, *storagev1.ListPartitionsRequest) (*storagev1.ListPartitionsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*storagev1.Partition, 0, len(s.partitions))
	for _, p := range s.partitions {
		out = append(out, &storagev1.Partition{
			Name:     p.name,
			Settings: append([]byte(nil), p.settings...),
		})
	}
	return &storagev1.ListPartitionsResponse{Partitions: out}, nil
}

func (s *Server) Put(_ context.Context, req *storagev1.PutRequest) (*storagev1.PutResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is empty; this server does not generate ids")
	}
	// A missing partition is refused rather than created. Creating one here
	// would turn a caller's typo into a silently empty partition nobody reads.
	p, ok := s.lookup(req.GetPartition())
	if !ok {
		return nil, notFoundPartition(req.GetPartition())
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, dup := p.byID[req.GetId()]; dup {
		return nil, status.Errorf(codes.AlreadyExists,
			"item %q is already present in partition %q", req.GetId(), p.name)
	}

	// The position is taken from the counter inside this same critical
	// section as the append, which is what makes the ordering requirement
	// hold: a Put that completed before this one began took a lower number.
	it := &item{
		id:       req.GetId(),
		position: p.next,
		value:    append([]byte(nil), req.GetValue()...),
	}
	p.next++
	p.items = append(p.items, it)
	p.byID[it.id] = it

	return &storagev1.PutResponse{Position: it.position}, nil
}

func (s *Server) Claim(_ context.Context, req *storagev1.ClaimRequest) (*storagev1.ClaimResponse, error) {
	p, ok := s.lookup(req.GetPartition())
	if !ok {
		return nil, notFoundPartition(req.GetPartition())
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Selection, hold and increment are one critical section. Two concurrent
	// calls cannot return the same item because neither can observe the other
	// midway: there is no window between choosing an item and marking it.
	for _, it := range p.items { // items are in position order, lowest first
		if !eligible(it, req.GetNow()) || it.attempts >= req.GetAttemptCeiling() {
			continue
		}
		hold := req.GetHoldUntil()
		it.holdUntil = &hold
		it.attempts++
		return &storagev1.ClaimResponse{Item: it.proto(p.name)}, nil
	}

	// Nothing eligible is a success carrying no item. An idle partition is the
	// ordinary state of most partitions most of the time.
	return &storagev1.ClaimResponse{}, nil
}

func (s *Server) Release(_ context.Context, req *storagev1.ReleaseRequest) (*storagev1.ReleaseResponse, error) {
	p, ok := s.lookup(req.GetPartition())
	if !ok {
		return nil, notFoundPartition(req.GetPartition())
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	it, err := p.fenced(req.GetId(), req.GetExpectedAttempts())
	if err != nil {
		return nil, err
	}
	it.holdUntil = nil // the attempt count is deliberately untouched
	return &storagev1.ReleaseResponse{}, nil
}

func (s *Server) Delete(_ context.Context, req *storagev1.DeleteRequest) (*storagev1.DeleteResponse, error) {
	p, ok := s.lookup(req.GetPartition())
	if !ok {
		return nil, notFoundPartition(req.GetPartition())
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	it, err := p.fenced(req.GetId(), req.GetExpectedAttempts())
	if err != nil {
		return nil, err
	}
	p.remove(it)
	return &storagev1.DeleteResponse{}, nil
}

func (s *Server) Move(_ context.Context, req *storagev1.MoveRequest) (*storagev1.MoveResponse, error) {
	src, ok := s.lookup(req.GetSourcePartition())
	if !ok {
		return nil, notFoundPartition(req.GetSourcePartition())
	}
	dst, ok := s.lookup(req.GetDestinationPartition())
	if !ok {
		return nil, notFoundPartition(req.GetDestinationPartition())
	}

	// Both partitions are locked for the whole operation, in name order, so
	// that two concurrent moves between the same pair in opposite directions
	// cannot each hold what the other wants. Nothing observes the item absent
	// from both partitions or present in both, because nothing can take either
	// lock while this holds both.
	unlock := lockPair(src, dst)
	defer unlock()

	it, ok := src.byID[req.GetId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound,
			"item %q is not in partition %q", req.GetId(), src.name)
	}
	if _, clash := dst.byID[req.GetId()]; clash {
		return nil, status.Errorf(codes.AlreadyExists,
			"item %q is already present in partition %q", req.GetId(), dst.name)
	}
	if src == dst {
		// Source and destination are the same partition, and the id is in it.
		// The clash check above already refused this, so this is unreachable;
		// it is here so that a future edit cannot make it silently a no-op.
		return nil, status.Errorf(codes.AlreadyExists,
			"item %q is already present in partition %q", req.GetId(), dst.name)
	}

	src.remove(it)

	// The id, the value and the attempt count cross unchanged; the position is
	// the destination's to assign, and the hold does not travel.
	moved := &item{
		id:       it.id,
		position: dst.next,
		value:    it.value,
		attempts: it.attempts,
	}
	dst.next++
	dst.items = append(dst.items, moved)
	dst.byID[moved.id] = moved

	return &storagev1.MoveResponse{}, nil
}

func (s *Server) Scan(_ context.Context, req *storagev1.ScanRequest) (*storagev1.ScanResponse, error) {
	p, ok := s.lookup(req.GetPartition())
	if !ok {
		return nil, notFoundPartition(req.GetPartition())
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	limit := int(req.GetLimit())
	out := make([]*storagev1.Item, 0, min(limit, len(p.items)))
	for _, it := range p.items { // position order, lowest first
		if len(out) >= limit {
			break
		}
		// Nothing here writes. A scanned item is still claimable, exactly as
		// though the scan had not happened.
		if eligible(it, req.GetNow()) && it.attempts >= req.GetAttemptFloor() {
			out = append(out, it.proto(p.name))
		}
	}
	return &storagev1.ScanResponse{Items: out}, nil
}

// fenced returns the named item if its attempt count is the one the caller
// named. The read of the count and the caller's write happen under the same
// lock, so nothing can claim the item in between and make the check stale.
//
// It is called with the partition's mutex held.
func (p *partition) fenced(id string, expected uint32) (*item, error) {
	it, ok := p.byID[id]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "item %q is not in partition %q", id, p.name)
	}
	if it.attempts != expected {
		return nil, status.Errorf(codes.FailedPrecondition,
			"item %q in partition %q is at attempt %d, and the call named attempt %d",
			id, p.name, it.attempts, expected)
	}
	return it, nil
}

// remove takes an item out of a partition. It is reached from Delete and from
// the source half of Move, and from nowhere else — nothing expires, evicts or
// trims, on any bound.
//
// It is called with the partition's mutex held.
func (p *partition) remove(it *item) {
	delete(p.byID, it.id)
	for i, cur := range p.items {
		if cur == it {
			p.items = append(p.items[:i], p.items[i+1:]...)
			return
		}
	}
}

// eligible reports whether an item's hold is absent or at or before the
// instant the caller gave. The comparison is between two numbers the caller
// supplied; this server does not know either of them is a time.
func eligible(it *item, now int64) bool {
	return it.holdUntil == nil || *it.holdUntil <= now
}

// lockPair locks two partitions in name order and returns their unlock. Taking
// them in a defined order is what stops two moves between the same pair, one
// each way, from deadlocking.
func lockPair(a, b *partition) func() {
	if a == b {
		a.mu.Lock()
		return a.mu.Unlock
	}
	first, second := a, b
	if first.name > second.name {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	return func() {
		second.mu.Unlock()
		first.mu.Unlock()
	}
}

// proto renders an item for the wire. It copies, so that a caller holding the
// response cannot reach into the store.
//
// It is called with the partition's mutex held.
func (it *item) proto(partition string) *storagev1.Item {
	out := &storagev1.Item{
		Partition: partition,
		Id:        it.id,
		Position:  it.position,
		Value:     append([]byte(nil), it.value...),
		Attempts:  it.attempts,
	}
	if it.holdUntil != nil {
		hold := *it.holdUntil
		out.HoldUntil = &hold
	}
	return out
}

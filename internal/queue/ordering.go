package queue

import "sync"

// PositionWatch detects a backend that assigns positions out of order.
//
// The requirement it checks is stated at Put: within one partition, an item
// whose Put completed before another's began has the lower position. So the
// check compares a returned position only against positions whose calls had
// already completed when this one was issued — which is what Begin captures.
// Two Puts that overlap may legitimately complete in either order, and such a
// pair is not a violation.
//
// The check does not belong at Claim. A claimed position lower than one
// claimed before it is ordinary and required: a redelivered message keeps its
// position and is claimed ahead of newer work, so a check there would fire on
// every retry.
//
// The mark is in-memory and best-effort. It resets on restart, after which
// detection resumes from the first Put to each partition, and no SPI call is
// to be added to make it durable — a diagnostic does not earn a place in the
// contract every deployment must implement.
type PositionWatch struct {
	// backend is the storage target, named in every report so that an
	// operator learns which side of the boundary broke.
	backend string

	mu   sync.Mutex
	high map[string]Mark
}

// Mark is the highest position a completed Put to one partition returned, and
// whether any has completed at all.
type Mark struct {
	position int64
	set      bool
}

// Violation is a backend breaking the ordering requirement. It is reported and
// service continues: taking a queue out of service would convert an ordering
// fault we cannot fix into an outage we caused.
type Violation struct {
	Backend   string
	Partition string
	// Earlier is the position of a Put that had already completed when the
	// offending one was issued.
	Earlier int64
	// Returned is the position the offending Put came back with.
	Returned int64
}

// NewPositionWatch returns a watch that names backend in what it reports.
func NewPositionWatch(backend string) *PositionWatch {
	return &PositionWatch{backend: backend, high: make(map[string]Mark)}
}

// Begin is called before a Put is issued. It captures what had already
// completed, which is the only thing the returned position may be compared
// against.
func (w *PositionWatch) Begin(partition string) Mark {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.high[partition]
}

// Complete is called with the position the Put returned. It reports a
// violation, or nil, and records the position either way.
func (w *PositionWatch) Complete(partition string, before Mark, position int64) *Violation {
	w.mu.Lock()
	defer w.mu.Unlock()

	if cur := w.high[partition]; !cur.set || position > cur.position {
		w.high[partition] = Mark{position: position, set: true}
	}

	if before.set && position <= before.position {
		return &Violation{
			Backend:   w.backend,
			Partition: partition,
			Earlier:   before.position,
			Returned:  position,
		}
	}
	return nil
}

// Forget drops a partition's mark. Used when a queue stops being served, so
// that the map does not grow with names nothing writes to any more.
func (w *PositionWatch) Forget(partition string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.high, partition)
}

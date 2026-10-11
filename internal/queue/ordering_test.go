package queue_test

import (
	"testing"

	"github.com/koppathachan/kyu/internal/queue"
)

// The high-water mark compares only against Puts that had already completed.
func TestPositionWatchReportsABackwardsPosition(t *testing.T) {
	w := queue.NewPositionWatch("backend:1234")

	before := w.Begin("orders")
	if v := w.Complete("orders", before, 10); v != nil {
		t.Fatalf("the first put reported a violation: %+v", v)
	}

	before = w.Begin("orders")
	v := w.Complete("orders", before, 4)
	if v == nil {
		t.Fatal("a position that went backwards was not reported")
	}
	if v.Backend != "backend:1234" || v.Partition != "orders" || v.Earlier != 10 || v.Returned != 4 {
		t.Fatalf("the report is %+v, want it to name the backend, the partition and both positions", v)
	}
}

// Concurrent puts may complete in either order, and that is not a violation:
// the requirement constrains non-overlapping calls only.
func TestPositionWatchIgnoresOverlappingPuts(t *testing.T) {
	w := queue.NewPositionWatch("backend:1234")

	// Both calls are issued before either completes.
	firstMark := w.Begin("orders")
	secondMark := w.Begin("orders")

	if v := w.Complete("orders", secondMark, 20); v != nil {
		t.Fatalf("the higher position reported a violation: %+v", v)
	}
	if v := w.Complete("orders", firstMark, 19); v != nil {
		t.Fatalf("two overlapping puts completing out of order were reported: %+v", v)
	}
}

// Partitions do not share a mark.
func TestPositionWatchIsPerPartition(t *testing.T) {
	w := queue.NewPositionWatch("backend:1234")

	w.Complete("orders", w.Begin("orders"), 100)
	if v := w.Complete("invoices", w.Begin("invoices"), 1); v != nil {
		t.Fatalf("one partition's positions were compared against another's: %+v", v)
	}

	// And a forgotten partition starts over.
	w.Forget("orders")
	if v := w.Complete("orders", w.Begin("orders"), 1); v != nil {
		t.Fatalf("a forgotten partition kept its mark: %+v", v)
	}
}

package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/koppathachan/kyu/internal/engine"
	"github.com/koppathachan/kyu/internal/queue"
)

// 9.8: a message that reaches the ceiling is transferred with no consumer
// present.
func TestAnExhaustedMessageIsMovedWithNoConsumerPresent(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("orders", 10*time.Second, 2)
	enqueued := f.enqueue("orders", "m1")

	// Two deliveries spend the message's attempts.
	for want := uint32(1); want <= 2; want++ {
		msg := f.mustDequeue("orders")
		if msg.GetAttempt() != want {
			t.Fatalf("attempt %d, want %d", msg.GetAttempt(), want)
		}
		f.clock.advance(11 * time.Second)
	}

	// Nobody asks for anything from here on.
	eventually(t, "the exhausted message to reach the dead letter partition", func() bool {
		ids := f.dlqContents("orders")
		return len(ids) == 1 && ids[0] == enqueued.GetId()
	})

	// It kept its id, its payload and its attempt count.
	items := f.storage.contents(queue.DLQName("orders"))
	if len(items) != 1 {
		t.Fatalf("the dead letter partition holds %d items, want 1", len(items))
	}
	if got := items[0]; got.GetAttempts() != 2 || string(got.GetValue()) != "m1" {
		t.Fatalf("the transferred message is %+v, want payload m1 at attempt 2", got)
	}
}

// 9.8: sweeps resume after a restart, for queues created before the engine
// started.
func TestSweepsResumeAfterARestart(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 1)
	f.enqueue("orders", "m1")
	f.mustDequeue("orders")
	f.clock.advance(11 * time.Second)

	if len(f.dlqContents("orders")) != 0 {
		t.Fatal("something transferred the message with no reaper running")
	}

	// A fresh engine, which learns of the queue only from ListPartitions.
	f = f.restart()

	eventually(t, "the restarted engine to sweep a queue it did not create", func() bool {
		return len(f.dlqContents("orders")) == 1
	})
}

// 9.8: no sweep runs for a dead letter partition. There is nowhere for a
// message to move on from one.
func TestDeadLetterPartitionsAreNotSwept(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("orders", 10*time.Second, 1)
	f.createQueue("invoices", 10*time.Second, 1)

	// Plant a message in a dead letter partition, at the ceiling, with its
	// hold already lapsed — everything a sweep looks for. If anything swept
	// dead letter partitions, this is what it would move on.
	ctx := context.Background()
	if _, err := f.storage.client.Put(ctx, queue.DLQName("orders"), "spent", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := f.storage.client.Claim(ctx, queue.DLQName("orders"),
		f.clock.now().UnixNano(), f.clock.now().Add(-time.Hour).UnixNano(), 99); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// Both live queues are swept repeatedly over the same period, so what
	// follows is the absence of sweeping rather than the absence of time.
	eventually(t, "both queues to be swept", func() bool {
		return f.storage.scansOf("orders") > 0 && f.storage.scansOf("invoices") > 0
	})

	for name, n := range map[string]int{
		queue.DLQName("orders"):   f.storage.scansOf(queue.DLQName("orders")),
		queue.DLQName("invoices"): f.storage.scansOf(queue.DLQName("invoices")),
	} {
		if n != 0 {
			t.Fatalf("%q was scanned %d times, and it is the end of the line", name, n)
		}
	}

	// Nowhere for it to have gone, and it did not go there.
	if ids := f.storage.ids(queue.DLQName("orders")); len(ids) != 1 || ids[0] != "spent" {
		t.Fatalf("the dead letter partition holds %v, want the message left where it was", ids)
	}
	if _, ok := f.storage.partitions()[queue.DLQName(queue.DLQName("orders"))]; ok {
		t.Fatal("a dead letter partition of a dead letter partition was created")
	}
}

// 9.8: correctness does not depend on the reaper. With it disabled, no
// message is delivered past its ceiling.
func TestNoMessageIsDeliveredPastItsCeilingWithoutTheReaper(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 3)
	f.enqueue("orders", "m1")

	for want := uint32(1); want <= 3; want++ {
		msg := f.mustDequeue("orders")
		if msg.GetAttempt() != want {
			t.Fatalf("attempt %d, want %d", msg.GetAttempt(), want)
		}
		f.clock.advance(11 * time.Second)
	}

	// The message is still in the queue, awaiting a transfer nobody will
	// make, and no consumer can have it.
	for range 5 {
		if msg := f.dequeue("orders"); msg != nil {
			t.Fatalf("delivery %d happened past the ceiling", msg.GetAttempt())
		}
		f.clock.advance(time.Hour)
	}
	if len(f.dlqContents("orders")) != 0 {
		t.Fatal("something transferred the message with the reaper disabled")
	}
}

// 9.8: two engines sweeping one queue leave the message present exactly once.
// The loser's Move returns NOT_FOUND, which is ordinary.
func TestTwoEnginesSweepingOneQueueTransferItOnce(t *testing.T) {
	storage := startStorage(t)
	a := startEngine(t, storage)

	a.createQueue("orders", 10*time.Second, 1)
	for _, payload := range []string{"m1", "m2", "m3"} {
		a.enqueue("orders", payload)
		msg := a.mustDequeue("orders")
		if msg.GetAttempt() != 1 {
			t.Fatalf("attempt %d, want 1", msg.GetAttempt())
		}
	}

	// A second engine, sweeping the same queue, with both clocks past the
	// holds.
	b := startEngine(t, storage)
	a.clock.advance(11 * time.Second)
	b.clock.advance(11 * time.Second)

	eventually(t, "all three messages to be transferred", func() bool {
		return len(a.dlqContents("orders")) == 3
	})

	// Give both sweeps time to race over the same messages.
	time.Sleep(100 * time.Millisecond)

	ids := a.dlqContents("orders")
	if len(ids) != 3 {
		t.Fatalf("the dead letter partition holds %d messages, want 3", len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("message %q is in the dead letter partition more than once", id)
		}
		seen[id] = true
	}
}

// 9.3: a backlog larger than one scan is cleared over successive sweeps, and
// no message is skipped.
func TestABacklogIsClearedOverSeveralSweeps(t *testing.T) {
	storage := startStorage(t)

	cfg := testConfig(storage.addr)
	cfg.ScanLimit = 2 // smaller than the backlog, so several sweeps are needed
	f := startEngineWith(t, storage, cfg)

	f.createQueue("orders", 10*time.Second, 1)
	for range 7 {
		f.enqueue("orders", "m")
		f.mustDequeue("orders")
	}
	f.clock.advance(11 * time.Second)

	eventually(t, "the whole backlog to be transferred", func() bool {
		return len(f.dlqContents("orders")) == 7
	})
}

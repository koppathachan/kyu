package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/sasidakh/kyu/internal/engine"
	kyuv1 "github.com/sasidakh/kyu/kyu/v1"
	"google.golang.org/grpc/codes"
)

// 7.5: a held message is invisible to a second consumer, and reappears when
// its hold lapses.
func TestAHeldMessageIsInvisibleUntilItsHoldLapses(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 30*time.Second, 5)
	f.enqueue("orders", "m1")

	first := f.mustDequeue("orders")
	if msg := f.dequeue("orders"); msg != nil {
		t.Fatalf("a second consumer received %q while it was held", msg.GetId())
	}

	// Still held one instant before the hold.
	f.clock.advance(30*time.Second - time.Nanosecond)
	if msg := f.dequeue("orders"); msg != nil {
		t.Fatal("the message was delivered before its hold lapsed")
	}

	f.clock.advance(time.Nanosecond)
	again := f.mustDequeue("orders")
	if again.GetId() != first.GetId() {
		t.Fatalf("redelivery returned %q, want %q", again.GetId(), first.GetId())
	}
}

// 7.5: attempts rise by exactly one per claim, and continue across a restart
// because the engine keeps no counter of its own.
func TestAttemptsCountClaimsAndSurviveARestart(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	f.enqueue("orders", "m1")

	for want := uint32(1); want <= 3; want++ {
		msg := f.mustDequeue("orders")
		if msg.GetAttempt() != want {
			t.Fatalf("attempt %d, want %d", msg.GetAttempt(), want)
		}
		f.clock.advance(11 * time.Second)
	}

	f = f.restart(engine.WithSweeping(false))

	msg := f.mustDequeue("orders")
	if msg.GetAttempt() != 4 {
		t.Fatalf("attempt %d after a restart, want 4 — the count continues from storage", msg.GetAttempt())
	}
}

// 7.5: an unacknowledged message is never deleted.
func TestAnUnacknowledgedMessageIsNeverDeleted(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 99)
	f.enqueue("orders", "m1")

	for range 5 {
		f.mustDequeue("orders")
		f.clock.advance(11 * time.Second)
	}

	if msg := f.dequeue("orders"); msg == nil {
		t.Fatal("the message is gone, and nothing acknowledged it")
	}
}

// 7.6: the slow consumer, end to end. A claims, A's hold lapses, B claims, A
// acknowledges — and A's settlement is refused rather than destroying the
// message B is working on.
func TestASlowConsumersAcknowledgementIsRefused(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	f.enqueue("orders", "m1")

	a := f.mustDequeue("orders")
	f.clock.advance(11 * time.Second) // A's hold lapses while A is still working
	b := f.mustDequeue("orders")

	if b.GetId() != a.GetId() || b.GetAttempt() != a.GetAttempt()+1 {
		t.Fatalf("B received %q at attempt %d, want %q at %d",
			b.GetId(), b.GetAttempt(), a.GetId(), a.GetAttempt()+1)
	}

	requireCode(t, f.ack(a), codes.Aborted)

	// The message is still there, and B's hold stands: nothing is claimable.
	if msg := f.dequeue("orders"); msg != nil {
		t.Fatalf("claimed %q, so the refused acknowledgement cleared B's hold", msg.GetId())
	}
	f.clock.advance(11 * time.Second)
	if msg := f.dequeue("orders"); msg == nil {
		t.Fatal("the message is gone, so the refused acknowledgement deleted it after all")
	}
}

// 7.6, the second half: the same sequence with the engine restarted between
// A's claim and A's acknowledgement. This is the case engine memory cannot
// cover, and the reason the attempt count travels in the lease token.
func TestASlowConsumersAcknowledgementIsRefusedAcrossARestart(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	f.enqueue("orders", "m1")

	a := f.mustDequeue("orders")

	f = f.restart(engine.WithSweeping(false))

	f.clock.advance(11 * time.Second)
	b := f.mustDequeue("orders")
	if b.GetAttempt() != a.GetAttempt()+1 {
		t.Fatalf("B is at attempt %d, want %d", b.GetAttempt(), a.GetAttempt()+1)
	}

	// A's token was issued by an engine that no longer exists.
	requireCode(t, f.ack(a), codes.Aborted)

	if msg := f.dequeue("orders"); msg != nil {
		t.Fatalf("claimed %q, so B's hold did not stand", msg.GetId())
	}
}

// 7.7: the other lost-settlement branch. A consumer holds a message at the
// ceiling, its hold lapses, the reaper moves it, and the consumer then
// acknowledges.
func TestAnAcknowledgementAfterADeadLetterTransferIsRefused(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("orders", 10*time.Second, 1)
	f.enqueue("orders", "m1")

	// The one delivery this queue allows. The message is now at its ceiling
	// and undeliverable, but it is still held.
	msg := f.mustDequeue("orders")
	if msg.GetAttempt() != 1 {
		t.Fatalf("attempt %d, want 1", msg.GetAttempt())
	}

	// The hold lapses while the consumer is still working, and the reaper
	// moves the message out from under it.
	f.clock.advance(11 * time.Second)
	eventually(t, "the message to reach the dead letter partition", func() bool {
		return len(f.dlqContents("orders")) == 1
	})

	// The consumer finishes and acknowledges.
	err := f.ack(msg)
	requireCode(t, err, codes.Aborted)

	// It is in the dead letter partition exactly once, and not in the queue.
	if ids := f.dlqContents("orders"); len(ids) != 1 || ids[0] != msg.GetId() {
		t.Fatalf("the dead letter partition holds %v, want exactly %q", ids, msg.GetId())
	}
	if got := f.dequeue("orders"); got != nil {
		t.Fatalf("the queue still holds %q", got.GetId())
	}
}

// A repeated acknowledgement is refused rather than confirmed: nothing in the
// SPI distinguishes a consumer's own earlier settlement from a transfer.
func TestARepeatedAcknowledgementIsRefused(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 5)
	f.enqueue("orders", "m1")

	msg := f.mustDequeue("orders")
	f.mustAck(msg)
	requireCode(t, f.ack(msg), codes.Aborted)
}

// An empty queue is a success carrying no message, never an error.
func TestDequeueOnAnEmptyQueueSucceeds(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 0, 0)

	resp, err := f.client.Dequeue(context.Background(), &kyuv1.DequeueRequest{Queue: "orders"})
	if err != nil {
		t.Fatalf("Dequeue on an empty queue: %v", err)
	}
	if resp.GetMessage() != nil {
		t.Fatalf("an empty queue returned %v", resp.GetMessage())
	}
}

// A lease token this engine never issued is a client error, not a lost race.
func TestAMalformedLeaseTokenIsRefused(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	_, err := f.client.Ack(context.Background(), &kyuv1.AckRequest{LeaseToken: []byte("not a token")})
	requireCode(t, err, codes.InvalidArgument)
}

// Messages are delivered in the order storage assigned, and a redelivered
// message keeps its place in line.
func TestOrderingAndRedelivery(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	first := f.enqueue("orders", "m1")
	second := f.enqueue("orders", "m2")
	if second.GetPosition() <= first.GetPosition() {
		t.Fatalf("positions %d then %d, want the second higher", first.GetPosition(), second.GetPosition())
	}

	one := f.mustDequeue("orders")
	if one.GetId() != first.GetId() {
		t.Fatalf("claimed %q first, want the message enqueued first", one.GetId())
	}

	// The one behind it is claimable while the first is held.
	two := f.mustDequeue("orders")
	if two.GetId() != second.GetId() {
		t.Fatalf("claimed %q, want the message behind the held one", two.GetId())
	}

	// When the first message's hold lapses it is claimed ahead of the newer
	// one, whose hold lapses at the same instant.
	f.clock.advance(11 * time.Second)
	again := f.mustDequeue("orders")
	if again.GetId() != first.GetId() {
		t.Fatalf("redelivery returned %q, want the oldest message to keep its place", again.GetId())
	}
}

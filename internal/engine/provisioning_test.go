package engine_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/koppathachan/kyu/internal/queue"
	kyuv1 "github.com/koppathachan/kyu/kyu/v1"
	"google.golang.org/grpc/codes"
)

// 6.6: creating a queue creates its dead letter companion.
func TestCreateQueueCreatesBothPartitions(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("orders", 0, 0)

	partitions := f.storage.partitions()
	if _, ok := partitions["orders"]; !ok {
		t.Fatalf("partitions are %v, want one named orders", keysOf(partitions))
	}
	if _, ok := partitions["orders.dlq"]; !ok {
		t.Fatalf("partitions are %v, want a dead letter companion", keysOf(partitions))
	}
}

// 6.6: the reserved suffix, at creation and at enqueue.
func TestReservedNamesAreRefused(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	_, err := f.client.CreateQueue(context.Background(), &kyuv1.CreateQueueRequest{Queue: "orders.dlq"})
	requireCode(t, err, codes.InvalidArgument)

	if partitions := f.storage.partitions(); len(partitions) != 0 {
		t.Fatalf("partitions %v exist, want none created", keysOf(partitions))
	}

	f.createQueue("orders", 0, 0)
	_, err = f.client.Enqueue(context.Background(), &kyuv1.EnqueueRequest{
		Queue: "orders.dlq", Payload: []byte("x"),
	})
	requireCode(t, err, codes.InvalidArgument)
}

// 6.6: creating a queue that exists is an error.
func TestCreateQueueRefusesARepeat(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("orders", 10*time.Second, 2)

	_, err := f.client.CreateQueue(context.Background(), &kyuv1.CreateQueueRequest{Queue: "orders"})
	requireCode(t, err, codes.AlreadyExists)

	// The first queue's settings are unchanged: its ceiling is still two.
	f.enqueue("orders", "m1")
	for i := range 2 {
		if msg := f.dequeue("orders"); msg == nil {
			t.Fatalf("claim %d returned nothing, want the message", i)
		}
		f.clock.advance(11 * time.Second)
	}
	if msg := f.dequeue("orders"); msg != nil {
		t.Fatalf("a third delivery happened, so the ceiling is not the two it was created with")
	}
}

// 6.6: settings are read back through ListPartitions after a restart. The
// engine keeps them nowhere else.
func TestSettingsSurviveAnEngineRestart(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("orders", 10*time.Second, 2)
	f.enqueue("orders", "m1")

	// A fresh engine, sharing no memory with the first.
	f = f.restart()

	// The settings it applies are the ones it read: a hold of ten seconds and
	// a ceiling of two. Nothing but ListPartitions could have told it.
	for i := range 2 {
		if msg := f.dequeue("orders"); msg == nil {
			t.Fatalf("claim %d returned nothing", i)
		}
		f.clock.advance(11 * time.Second)
	}
	if msg := f.dequeue("orders"); msg != nil {
		t.Fatal("a third delivery happened after the restart, so the ceiling was not read back")
	}
}

// Defaults are recorded at creation, not assumed at read time.
func TestDefaultsAreRecordedAtCreation(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("orders", 0, 0)

	for name, encoded := range f.storage.partitions() {
		settings, err := queue.DecodeSettings(encoded)
		if err != nil {
			t.Fatalf("partition %q was created with settings that will not decode: %v", name, err)
		}
		if settings.HoldDuration != queue.DefaultHoldDuration || settings.AttemptCeiling != queue.DefaultAttemptCeiling {
			t.Fatalf("partition %q recorded %+v, want the defaults", name, settings)
		}
	}
}

// 6.2b: a queue created on another replica is served here.
func TestAQueueCreatedOnAnotherReplicaIsDiscovered(t *testing.T) {
	storage := startStorage(t)
	a := startEngine(t, storage)
	b := startEngine(t, storage)

	b.createQueue("orders", 0, 0)

	// A has never heard of it, and must not conclude it does not exist.
	resp := a.enqueue("orders", "m1")
	if resp.GetId() == "" {
		t.Fatal("the enqueue was served but carried no id")
	}
	// It adopted the queue rather than merely answering once: the message is
	// deliverable here, and this engine now sweeps a queue it never created.
	if msg := a.mustDequeue("orders"); msg.GetId() != resp.GetId() {
		t.Fatalf("delivered %q, want the message just enqueued", msg.GetId())
	}
	eventually(t, "the discovered queue to be swept here", func() bool {
		return storage.scansOf("orders") > 0
	})
}

// 6.2b: a queue that really is absent is still refused.
func TestAnAbsentQueueIsRefused(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	_, err := f.client.Enqueue(context.Background(), &kyuv1.EnqueueRequest{Queue: "absent"})
	requireCode(t, err, codes.NotFound)
}

// 6.2b: a remembered absence does not outlive its interval.
func TestARememberedAbsenceExpires(t *testing.T) {
	storage := startStorage(t)

	// Discovery's own timer is pushed out of the way, so that what is being
	// tested is the negative cache and the client path alone.
	cfg := testConfig(storage.addr)
	cfg.DiscoveryInterval = time.Hour
	a := startEngineWith(t, storage, cfg)
	b := startEngine(t, storage)

	_, err := a.client.Enqueue(context.Background(), &kyuv1.EnqueueRequest{Queue: "orders"})
	requireCode(t, err, codes.NotFound)

	// Created elsewhere, a moment after A answered.
	b.createQueue("orders", 0, 0)

	// Within the interval, A still serves the absence it remembers.
	_, err = a.client.Enqueue(context.Background(), &kyuv1.EnqueueRequest{Queue: "orders"})
	requireCode(t, err, codes.NotFound)

	// Past it, A looks again and finds the queue.
	a.clock.advance(cfg.NegativeCacheTTL + time.Second)
	a.enqueue("orders", "m1")
}

// 6.2b: an absence contradicted by this engine's own creation is not a race,
// and must not refuse the client that just created the queue.
func TestCreatingAQueueClearsItsRememberedAbsence(t *testing.T) {
	storage := startStorage(t)

	cfg := testConfig(storage.addr)
	cfg.DiscoveryInterval = time.Hour
	f := startEngineWith(t, storage, cfg)

	_, err := f.client.Enqueue(context.Background(), &kyuv1.EnqueueRequest{Queue: "orders"})
	requireCode(t, err, codes.NotFound)

	f.createQueue("orders", 0, 0)
	f.enqueue("orders", "m1") // immediately, with no TTL waited out
}

// 6.2c: a queue whose settings this engine cannot read is refused, not
// guessed at — and the others keep being served.
func TestAnUnreadableQueueIsRefusedAndTheOthersAreServed(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	f.createQueue("readable", 0, 0)

	// A queue as a newer engine might have written it: present, with settings
	// this engine has no version for.
	ctx := context.Background()
	for _, name := range []string{"newer", "newer.dlq"} {
		if err := f.storage.client.CreatePartition(ctx, name, []byte{0xff, 0xff, 0xff}); err != nil {
			t.Fatalf("CreatePartition(%q): %v", name, err)
		}
	}

	_, err := f.client.Enqueue(ctx, &kyuv1.EnqueueRequest{Queue: "newer", Payload: []byte("m1")})
	requireCode(t, err, codes.FailedPrecondition)

	_, err = f.client.Dequeue(ctx, &kyuv1.DequeueRequest{Queue: "newer"})
	requireCode(t, err, codes.FailedPrecondition)

	// No sweep is issued against it either. The readable queue is swept
	// repeatedly over the same period, so this is the absence of sweeping
	// rather than the absence of time passing.
	eventually(t, "the readable queue to be swept", func() bool {
		return f.storage.scansOf("readable") > 0
	})
	if n := f.storage.scansOf("newer"); n != 0 {
		t.Fatalf("a queue whose ceiling this engine cannot read was scanned %d times", n)
	}

	// One queue written by a newer engine does not take the others down.
	f.enqueue("readable", "m1")
	if msg := f.dequeue("readable"); msg == nil {
		t.Fatal("the readable queue stopped being served")
	}
}

// 9.9: discovery across replicas, including after the replica that created
// the queue has gone away — the case startup-only enumeration failed at
// silently.
func TestAQueueOutlivesTheReplicaThatCreatedIt(t *testing.T) {
	storage := startStorage(t)
	a := startEngine(t, storage)
	b := startEngine(t, storage)

	b.createQueue("orders", 10*time.Second, 1)

	// A serves it although it has never seen it created.
	a.enqueue("orders", "m1")

	// B goes away. Nothing but A is left to sweep the queue.
	b.stop()

	msg := a.mustDequeue("orders")
	if msg.GetAttempt() != 1 {
		t.Fatalf("attempt %d on the first delivery, want 1", msg.GetAttempt())
	}
	a.clock.advance(11 * time.Second) // the hold lapses at the ceiling

	eventually(t, "the exhausted message to reach the dead letter partition", func() bool {
		return len(a.dlqContents("orders")) == 1
	})
}

// A queue whose companion could not be created is not reported as created.
// The companion is created first, so a creation that fails part-way leaves
// nothing behind claiming to be a queue.
func TestAHalfCreatedQueueIsNotReportedAsCreated(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage)

	storage.fail(true)
	_, err := f.client.CreateQueue(context.Background(), &kyuv1.CreateQueueRequest{Queue: "orders"})
	if err == nil {
		t.Fatal("a queue was created although its dead letter companion could not be")
	}

	storage.fail(false)
	if partitions := f.storage.partitions(); len(partitions) != 0 {
		t.Fatalf("partitions %v exist after a failed creation, want none", keysOf(partitions))
	}

	// And the name is free: a retry creates the queue properly.
	f.createQueue("orders", 0, 0)
	f.enqueue("orders", "m1")
}

// keysOf names a partition set for a failure message.
func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

package engine_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/sasidakh/kyu/internal/engine"
	kyuv1 "github.com/sasidakh/kyu/kyu/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 10.5: an unreachable backend produces one reported failure and no retry.
//
// Retrying against a backend that is down converts one failure into a stream
// of them and delays the operator learning that anything is wrong.
func TestADownedBackendIsReportedOnceAndNotRetried(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 0, 0)

	storage.fail(true)
	storage.resetCounts()

	_, err := f.client.Dequeue(context.Background(), &kyuv1.DequeueRequest{Queue: "orders"})
	requireCode(t, err, codes.Unavailable)

	if n := storage.callsTo("Claim"); n != 1 {
		t.Fatalf("the engine issued %d claims for one dequeue, want exactly 1 — a downed backend is not retried", n)
	}

	// The failure names where it came from, so that an operator is not left
	// guessing which side of the boundary broke.
	msg := status.Convert(err).Message()
	for _, want := range []string{"Claim", "orders", storage.addr} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the reported failure %q does not name %q", msg, want)
		}
	}
}

// 10.4: an SPI failure is attributable, whichever call produced it.
func TestSPIFailuresNameTheOperationAndPartition(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 0, 0)
	storage.fail(true)

	_, err := f.client.Enqueue(context.Background(), &kyuv1.EnqueueRequest{
		Queue: "orders", Payload: []byte("m1"),
	})
	requireCode(t, err, codes.Unavailable)

	msg := status.Convert(err).Message()
	for _, want := range []string{"Put", "orders", storage.addr} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the reported failure %q does not name %q", msg, want)
		}
	}
}

// 10.5: a cancelled stream is not a fault. Transport statuses are outside the
// outcome table and outside what the engine reports.
func TestACancelledStreamIsNotReportedAsAFault(t *testing.T) {
	storage := startStorage(t)
	log := &recorder{}
	f := startEngine(t, storage, engine.WithSweeping(false), engine.WithLogger(slog.New(log)))

	f.createQueue("orders", time.Hour, 9)
	f.enqueue("orders", "m1")

	c := f.openStream("orders", 1)
	c.mustRecvMessage(arrives)
	c.break_()

	// The message comes back, which is the release the broken stream issued.
	eventually(t, "the cancelled stream's message to be claimable", func() bool {
		return f.dequeue("orders") != nil
	})

	if reported := log.reported(); len(reported) != 0 {
		t.Fatalf("a cancelled stream was reported as a fault: %v", reported)
	}
}

// 10.5: a status named in the table is not reused for an outcome the table
// does not name it for.
func TestEachOutcomeCarriesItsOwnStatus(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	ctx := context.Background()

	// A queue that exists and is unreadable, for the FAILED_PRECONDITION row.
	if err := f.storage.client.CreatePartition(ctx, "newer", []byte("not this engine's")); err != nil {
		t.Fatalf("CreatePartition: %v", err)
	}

	// A message whose hold has passed to another consumer, for ABORTED.
	f.enqueue("orders", "m1")
	stale := f.mustDequeue("orders")
	f.clock.advance(11 * time.Second)
	f.mustDequeue("orders")

	cases := []struct {
		name string
		want codes.Code
		call func() error
	}{
		{
			name: "a reserved name is an argument error, not a conflict",
			want: codes.InvalidArgument,
			call: func() error {
				_, err := f.client.CreateQueue(ctx, &kyuv1.CreateQueueRequest{Queue: "x.dlq"})
				return err
			},
		},
		{
			name: "a queue that does not exist",
			want: codes.NotFound,
			call: func() error {
				_, err := f.client.Enqueue(ctx, &kyuv1.EnqueueRequest{Queue: "absent"})
				return err
			},
		},
		{
			name: "a queue that already exists",
			want: codes.AlreadyExists,
			call: func() error {
				_, err := f.client.CreateQueue(ctx, &kyuv1.CreateQueueRequest{Queue: "orders"})
				return err
			},
		},
		{
			name: "a queue whose settings this engine cannot read",
			want: codes.FailedPrecondition,
			call: func() error {
				_, err := f.client.Enqueue(ctx, &kyuv1.EnqueueRequest{Queue: "newer"})
				return err
			},
		},
		{
			name: "a settlement for a hold that has passed on",
			want: codes.Aborted,
			call: func() error { return f.ack(stale) },
		},
		{
			name: "a lease token this engine never issued",
			want: codes.InvalidArgument,
			call: func() error {
				_, err := f.client.Ack(ctx, &kyuv1.AckRequest{LeaseToken: []byte("nonsense")})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireCode(t, tc.call(), tc.want)
		})
	}
}

// 10.2: nothing available is a result, not an error. No path returns
// NOT_FOUND for emptiness.
func TestEmptinessIsAlwaysASuccess(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	ctx := context.Background()

	// An empty queue.
	resp, err := f.client.Dequeue(ctx, &kyuv1.DequeueRequest{Queue: "orders"})
	if err != nil {
		t.Fatalf("Dequeue on an empty queue: %v", err)
	}
	if resp.GetMessage() != nil {
		t.Fatal("an empty queue carried a message")
	}

	// A queue whose only message is held: also empty, as far as a second
	// consumer is concerned.
	f.enqueue("orders", "m1")
	f.mustDequeue("orders")
	resp, err = f.client.Dequeue(ctx, &kyuv1.DequeueRequest{Queue: "orders"})
	if err != nil {
		t.Fatalf("Dequeue on a fully held queue: %v", err)
	}
	if resp.GetMessage() != nil {
		t.Fatal("a held message was delivered twice")
	}

	// And at the SPI: an empty claim and an empty scan are successes.
	if item, err := f.storage.client.Claim(ctx, "orders", f.clock.now().UnixNano(),
		f.clock.now().Add(time.Second).UnixNano(), 9); err != nil || item != nil {
		t.Fatalf("Claim returned (%v, %v), want no item and no error", item, err)
	}
	if items, err := f.storage.client.Scan(ctx, "orders", f.clock.now().UnixNano(), 99, 10); err != nil || len(items) != 0 {
		t.Fatalf("Scan returned (%v, %v), want nothing and no error", items, err)
	}
}

// 10.3: a settlement refused by its fence is ordinary for the engine. It is
// not reported as a fault and it is not retried.
func TestARefusedSettlementIsNotAFaultAndIsNotRetried(t *testing.T) {
	storage := startStorage(t)
	log := &recorder{}
	f := startEngine(t, storage, engine.WithSweeping(false), engine.WithLogger(slog.New(log)))

	f.createQueue("orders", 10*time.Second, 9)
	f.enqueue("orders", "m1")

	stale := f.mustDequeue("orders")
	f.clock.advance(11 * time.Second)
	f.mustDequeue("orders")

	storage.resetCounts()
	requireCode(t, f.ack(stale), codes.Aborted)

	if n := storage.callsTo("Delete"); n != 1 {
		t.Fatalf("the engine issued %d deletes, want exactly 1 — a refused settlement is not retried", n)
	}
	if reported := log.reported(); len(reported) != 0 {
		t.Fatalf("a refused settlement was reported as a fault: %v", reported)
	}
}

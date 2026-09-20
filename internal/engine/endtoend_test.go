package engine_test

import (
	"testing"
	"time"

	"github.com/sasidakh/kyu/internal/queue"
	kyuv1 "github.com/sasidakh/kyu/kyu/v1"
)

// consumer is one of the two dequeue forms, so that the whole journey below
// can be run through each of them and compared.
type consumer interface {
	// take receives one delivery.
	take(queue string) *kyuv1.Message
	// settle acknowledges a delivery, and fails the test if it is refused.
	settle(msg *kyuv1.Message)
}

// TestAMessageLivesItsWholeLife is the test this change exists to make pass.
//
// A queue is created, a message is enqueued, a consumer takes it and fails by
// doing nothing until its hold lapses, it comes back with its attempt count
// rising, it reaches the ceiling, and it arrives in the dead letter partition
// with no consumer asking for anything. Then a second message is enqueued,
// consumed and acknowledged, and is gone.
//
// It runs against the reference storage server over gRPC on a real socket,
// through each of the two dequeue forms.
func TestAMessageLivesItsWholeLife(t *testing.T) {
	const ceiling = 3
	const hold = 10 * time.Second

	forms := map[string]func(*engineFixture) consumer{
		"unary":     func(f *engineFixture) consumer { return unaryConsumer{f} },
		"streaming": func(f *engineFixture) consumer { return &streamingConsumer{f: f} },
	}

	for form, newConsumer := range forms {
		t.Run(form, func(t *testing.T) {
			storage := startStorage(t)
			f := startEngine(t, storage)
			c := newConsumer(f)

			f.createQueue("orders", hold, ceiling)
			failing := f.enqueue("orders", "the one that keeps failing")

			// Delivered once per attempt, failing each time by letting the
			// hold lapse. Nothing acknowledges and nothing is deleted.
			for attempt := uint32(1); attempt <= ceiling; attempt++ {
				msg := c.take("orders")
				if msg.GetId() != failing.GetId() {
					t.Fatalf("delivery %d carried %q, want %q", attempt, msg.GetId(), failing.GetId())
				}
				if msg.GetAttempt() != attempt {
					t.Fatalf("delivery %d says attempt %d", attempt, msg.GetAttempt())
				}
				if string(msg.GetPayload()) != "the one that keeps failing" {
					t.Fatalf("delivery %d carried the payload %q", attempt, msg.GetPayload())
				}
				f.clock.advance(hold + time.Second)
			}

			// Its attempts are spent. No consumer can have it, and no
			// consumer asks for it: the reaper moves it on its own.
			eventually(t, "the exhausted message to reach the dead letter partition", func() bool {
				ids := f.dlqContents("orders")
				return len(ids) == 1 && ids[0] == failing.GetId()
			})
			if got := f.dequeue("orders"); got != nil {
				t.Fatalf("the queue still delivered %q after the transfer", got.GetId())
			}

			// The transferred message kept everything but its position.
			items := f.storage.contents(queue.DLQName("orders"))
			if len(items) != 1 {
				t.Fatalf("the dead letter partition holds %d messages, want 1", len(items))
			}
			if got := items[0]; got.GetAttempts() != ceiling || string(got.GetValue()) != "the one that keeps failing" {
				t.Fatalf("the transferred message is %+v, want the payload and the attempt count it left with", got)
			}

			// A second message goes the ordinary way.
			succeeding := f.enqueue("orders", "the one that works")
			msg := c.take("orders")
			if msg.GetId() != succeeding.GetId() {
				t.Fatalf("delivered %q, want the message just enqueued", msg.GetId())
			}
			c.settle(msg)

			if got := f.dequeue("orders"); got != nil {
				t.Fatalf("the acknowledged message came back as %q", got.GetId())
			}
			if remainingInQueue := f.storage.contents("orders"); len(remainingInQueue) != 0 {
				t.Fatalf("the queue still holds %d messages, want none", len(remainingInQueue))
			}
			// And the dead letter partition is untouched by any of that.
			if ids := f.dlqContents("orders"); len(ids) != 1 {
				t.Fatalf("the dead letter partition holds %v, want only the exhausted message", ids)
			}
		})
	}
}

// unaryConsumer takes messages with Dequeue and settles them with Ack.
type unaryConsumer struct{ f *engineFixture }

func (c unaryConsumer) take(queue string) *kyuv1.Message { return c.f.mustDequeue(queue) }

func (c unaryConsumer) settle(msg *kyuv1.Message) { c.f.mustAck(msg) }

// streamingConsumer takes messages over StreamingDequeue.
//
// Each delivery gets its own stream, which is what a consumer that fails
// looks like from here: it takes one message, does nothing with it, and the
// engine hears nothing more from it until its hold lapses.
type streamingConsumer struct {
	f      *engineFixture
	stream *streamConsumer
}

func (c *streamingConsumer) take(queue string) *kyuv1.Message {
	c.stream = c.f.openStream(queue, 1)
	return c.stream.mustRecvMessage(arrives)
}

func (c *streamingConsumer) settle(msg *kyuv1.Message) {
	c.f.t.Helper()
	if got := c.stream.settle(msg); got != "settled" {
		c.f.t.Fatalf("settling %q on the stream was %s", msg.GetId(), got)
	}
}

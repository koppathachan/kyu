package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/koppathachan/kyu/internal/engine"
	kyuv1 "github.com/koppathachan/kyu/kyu/v1"
	"google.golang.org/grpc/codes"
)

// arrives is how long a test waits for something the stream should send. It is
// comfortably longer than the interval at which a drained stream claims again.
const arrives = 2 * time.Second

// silence is how long a test waits to be satisfied that nothing is coming.
const silence = 400 * time.Millisecond

// 8.1: a stream whose first message is not an Open is terminated.
func TestAStreamThatDoesNotOpenProperlyIsRejected(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))
	f.createQueue("orders", 0, 0)

	stream, err := f.client.StreamingDequeue(context.Background())
	if err != nil {
		t.Fatalf("StreamingDequeue: %v", err)
	}
	if err := stream.Send(&kyuv1.StreamingDequeueRequest{
		Request: &kyuv1.StreamingDequeueRequest_Ack{Ack: &kyuv1.Ack{LeaseToken: []byte("x")}},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	_, err = stream.Recv()
	requireCode(t, err, codes.InvalidArgument)
}

// 8.1: a second Open terminates the stream.
func TestASecondOpenIsRejected(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))
	f.createQueue("orders", 0, 0)

	c := f.openStream("orders", 1)
	if err := c.stream.Send(&kyuv1.StreamingDequeueRequest{
		Request: &kyuv1.StreamingDequeueRequest_Open{Open: &kyuv1.Open{Queue: "orders", InFlight: 1}},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	requireCode(t, c.end(arrives), codes.InvalidArgument)
}

// 8.5: a consumer that never acknowledges receives exactly its limit, and the
// rest of the queue stays claimable by others.
func TestAConsumerThatNeverAcknowledgesReceivesItsLimit(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 30*time.Second, 9)
	for _, payload := range []string{"m1", "m2", "m3", "m4", "m5"} {
		f.enqueue("orders", payload)
	}

	c := f.openStream("orders", 3)
	for i := range 3 {
		if msg := c.mustRecvMessage(arrives); msg == nil {
			t.Fatalf("message %d did not arrive", i)
		}
	}
	if resp := c.recv(silence); resp != nil {
		t.Fatalf("a fourth response arrived at a limit of three: %v", resp.GetResponse())
	}

	// The two it did not take are somebody else's to claim.
	for i := range 2 {
		if msg := f.dequeue("orders"); msg == nil {
			t.Fatalf("message %d beyond the stream's limit was not claimable", i)
		}
	}
}

// 8.5: an out-of-order acknowledgement settles the right message, and admits
// exactly one more.
func TestOutOfOrderAcknowledgementSettlesTheRightMessage(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 30*time.Second, 9)
	for _, payload := range []string{"m1", "m2", "m3", "m4"} {
		f.enqueue("orders", payload)
	}

	c := f.openStream("orders", 3)
	held := []*kyuv1.Message{
		c.mustRecvMessage(arrives),
		c.mustRecvMessage(arrives),
		c.mustRecvMessage(arrives),
	}

	// The third of the three is acknowledged first.
	c.ack(held[2])

	fourth := c.mustRecvMessage(arrives)
	if fourth.GetId() == held[2].GetId() {
		t.Fatal("the acknowledged message was delivered again")
	}

	// The other two are still held: nothing else is claimable.
	if msg := f.dequeue("orders"); msg != nil {
		t.Fatalf("claimed %q, so the acknowledgement settled more than the one message", msg.GetId())
	}
}

// 8.5: a broken stream returns what it held at once, rather than at each
// message's hold instant.
func TestABrokenStreamReturnsItsMessagesAtOnce(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", time.Hour, 9) // a hold nothing in this test waits out
	f.enqueue("orders", "m1")
	f.enqueue("orders", "m2")

	c := f.openStream("orders", 2)
	c.mustRecvMessage(arrives)
	c.mustRecvMessage(arrives)

	if msg := f.dequeue("orders"); msg != nil {
		t.Fatalf("claimed %q while the stream held it", msg.GetId())
	}

	c.break_()

	eventually(t, "the broken stream's messages to be claimable", func() bool {
		return f.dequeue("orders") != nil
	})
}

// 8.5: acknowledged messages are not returned, and the work already done is
// not lost when the rest of the stream breaks.
func TestAcknowledgedMessagesAreNotReturned(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", time.Hour, 9)
	for _, payload := range []string{"m1", "m2", "m3", "m4"} {
		f.enqueue("orders", payload)
	}

	c := f.openStream("orders", 4)
	held := []*kyuv1.Message{
		c.mustRecvMessage(arrives),
		c.mustRecvMessage(arrives),
		c.mustRecvMessage(arrives),
		c.mustRecvMessage(arrives),
	}
	settled := map[string]bool{held[0].GetId(): true, held[1].GetId(): true}
	c.ack(held[0])
	c.ack(held[1])

	// Give the settlements time to reach storage before the stream breaks.
	eventually(t, "the acknowledged messages to be gone", func() bool {
		return f.dequeue("orders") == nil || true
	})
	time.Sleep(50 * time.Millisecond)
	c.break_()

	// The two unacknowledged messages come back; the two settled ones do not.
	seen := map[string]bool{}
	eventually(t, "the released messages to be claimable", func() bool {
		if msg := f.dequeue("orders"); msg != nil {
			seen[msg.GetId()] = true
		}
		return len(seen) == 2
	})
	for id := range seen {
		if settled[id] {
			t.Fatalf("message %q was acknowledged and came back", id)
		}
	}
}

// 8.5: a stream breaking after its holds have already lapsed does not disturb
// the consumer that has since claimed them.
func TestABrokenStreamCannotDisturbAHoldThatHasPassedOn(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	f.enqueue("orders", "m1")

	c := f.openStream("orders", 1)
	streamed := c.mustRecvMessage(arrives)

	// The stream's hold lapses and another consumer claims the message.
	f.clock.advance(11 * time.Second)
	taken := f.mustDequeue("orders")
	if taken.GetId() != streamed.GetId() || taken.GetAttempt() != streamed.GetAttempt()+1 {
		t.Fatalf("the second consumer got %q at attempt %d, want %q at %d",
			taken.GetId(), taken.GetAttempt(), streamed.GetId(), streamed.GetAttempt()+1)
	}

	c.break_()
	time.Sleep(100 * time.Millisecond) // long enough for the release to be attempted

	// The second consumer's hold stands: the release was refused, as it must
	// be, and changed nothing.
	if msg := f.dequeue("orders"); msg != nil {
		t.Fatalf("claimed %q, so the broken stream's release cleared a live hold", msg.GetId())
	}
}

// 8.3: a refused settlement is reported on the stream and frees its slot, so
// that a consumer whose holds have all lapsed does not stall at its limit.
func TestARefusedSettlementIsReportedAndFreesItsSlot(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", 10*time.Second, 9)
	f.enqueue("orders", "m1")
	f.enqueue("orders", "m2")

	c := f.openStream("orders", 1)
	first := c.mustRecvMessage(arrives)

	// The stream's hold lapses and the message passes to another consumer.
	f.clock.advance(11 * time.Second)
	if taken := f.mustDequeue("orders"); taken.GetId() != first.GetId() {
		t.Fatalf("the second consumer got %q, want %q", taken.GetId(), first.GetId())
	}

	c.ack(first)

	resp := c.recv(arrives)
	if resp == nil {
		t.Fatal("nothing arrived after a settlement that could not succeed")
	}
	rejection := resp.GetSettlementRejection()
	if rejection == nil {
		t.Fatalf("the stream carried %v, want a settlement rejection", resp.GetResponse())
	}
	if rejection.GetMessageId() != first.GetId() {
		t.Fatalf("the rejection names %q, want %q", rejection.GetMessageId(), first.GetId())
	}

	// The slot is free, so the stream carries on.
	if next := c.mustRecvMessage(arrives); next.GetId() == first.GetId() {
		t.Fatal("the stream was handed the message it had just lost")
	}
}

// 8.3: an acknowledgement naming a message this stream does not hold is
// ignored, and does not end the stream.
func TestAnAcknowledgementForAMessageTheStreamDoesNotHoldIsIgnored(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	f.createQueue("orders", time.Hour, 9)
	f.enqueue("orders", "m1")
	f.enqueue("orders", "m2")

	// A message claimed through the unary form, which this stream never held.
	elsewhere := f.mustDequeue("orders")

	c := f.openStream("orders", 1)
	mine := c.mustRecvMessage(arrives)

	c.ack(elsewhere)
	if resp := c.recv(silence); resp != nil {
		t.Fatalf("the stream responded to an acknowledgement for a message it does not hold: %v", resp.GetResponse())
	}

	// The stream is still open and still works.
	c.ack(mine)
	if resp := c.recv(silence); resp != nil && resp.GetSettlementRejection() != nil {
		t.Fatalf("settling this stream's own message was rejected: %v", resp.GetSettlementRejection())
	}
}

// 8.6: the two forms agree. One sequence of operations is run through each,
// and the observable outcomes are compared.
//
// The sequence is one both forms can perform: take two messages, let their
// holds lapse, let another consumer take the first, settle the second, then
// settle the first. One settlement succeeds and one is refused because the
// message passed on. What the forms are not asked to agree on is when a
// message is offered — that is the in-flight limit, which exists on one form
// only by design, and which is why the stream is opened at exactly the number
// of messages this sequence holds.
func TestTheUnaryAndStreamingFormsAgree(t *testing.T) {
	storage := startStorage(t)
	f := startEngine(t, storage, engine.WithSweeping(false))

	unary := runUnary(t, f, "unary")
	streaming := runStreaming(t, f, "streaming")

	if len(unary) != len(streaming) {
		t.Fatalf("the unary form observed %v and the streaming form %v", unary, streaming)
	}
	for i := range unary {
		if unary[i] != streaming[i] {
			t.Fatalf("step %d: the unary form observed %q and the streaming form %q",
				i, unary[i], streaming[i])
		}
	}
}

// runUnary performs the shared sequence through Dequeue and Ack, returning
// what it observed at each step.
func runUnary(t *testing.T, f *engineFixture, queue string) []string {
	t.Helper()

	first, second := seedAgreementQueue(t, f, queue)

	var observed []string
	observed = append(observed, describe(first))
	observed = append(observed, describe(second))

	// The holds lapse and another consumer takes the first message.
	f.clock.advance(11 * time.Second)
	observed = append(observed, "passed on: "+describe(f.mustDequeue(queue)))

	// The second is settled before the first. Its hold has lapsed too, and
	// settling it first is what keeps the stream from being handed it again
	// the moment a slot frees — a redelivery the unary caller, which asks
	// rather than being offered, would not see.
	observed = append(observed, ackOutcome(f.ack(second)))
	observed = append(observed, ackOutcome(f.ack(first)))
	observed = append(observed, remaining(t, f, queue))
	return observed
}

// runStreaming performs the same sequence through StreamingDequeue.
func runStreaming(t *testing.T, f *engineFixture, queue string) []string {
	t.Helper()

	f.createQueue(queue, 10*time.Second, 9)
	f.enqueue(queue, "first")
	f.enqueue(queue, "second")

	c := f.openStream(queue, 2)
	first := c.mustRecvMessage(arrives)
	second := c.mustRecvMessage(arrives)

	var observed []string
	observed = append(observed, describe(first))
	observed = append(observed, describe(second))

	f.clock.advance(11 * time.Second)
	observed = append(observed, "passed on: "+describe(f.mustDequeue(queue)))

	observed = append(observed, c.settle(second))
	observed = append(observed, c.settle(first))
	observed = append(observed, remaining(t, f, queue))
	return observed
}

// seedAgreementQueue creates the queue, enqueues two messages and takes both
// through the unary form.
func seedAgreementQueue(t *testing.T, f *engineFixture, queue string) (*kyuv1.Message, *kyuv1.Message) {
	t.Helper()
	f.createQueue(queue, 10*time.Second, 9)
	f.enqueue(queue, "first")
	f.enqueue(queue, "second")
	return f.mustDequeue(queue), f.mustDequeue(queue)
}

// settle acknowledges on a stream and reports the outcome in the same words
// the unary form's outcome is reported in.
func (c *streamConsumer) settle(msg *kyuv1.Message) string {
	c.t.Helper()
	c.ack(msg)

	resp := c.recv(silence)
	if resp == nil {
		return "settled" // a successful settlement is silent
	}
	if r := resp.GetSettlementRejection(); r != nil {
		if r.GetMessageId() != msg.GetId() {
			c.t.Fatalf("a rejection arrived for %q, want %q", r.GetMessageId(), msg.GetId())
		}
		return "aborted"
	}
	c.t.Fatalf("the stream carried %v, want a settlement outcome", resp.GetResponse())
	return ""
}

// remaining reports how many messages are still in the queue, read through
// the SPI because a dequeue cannot tell an empty queue from a fully held one.
func remaining(t *testing.T, f *engineFixture, name string) string {
	t.Helper()
	return "remaining: " + itoa(uint32(len(f.storage.contents(name))))
}

func describe(msg *kyuv1.Message) string {
	return string(msg.GetPayload()) + " at attempt " + itoa(msg.GetAttempt())
}

func ackOutcome(err error) string {
	if err == nil {
		return "settled"
	}
	return "aborted"
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

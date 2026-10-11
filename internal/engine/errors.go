package engine

import (
	"errors"

	"github.com/koppathachan/kyu/internal/queue"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Every status the engine returns to a client is chosen in this file, and
// nowhere else. `specs/error-model` requires that a status named in its table
// is never used for an outcome the table does not name it for, and a rule like
// that is only checkable if there is one place to check.
//
//	Status                | Meaning                          | Produced by
//	----------------------|----------------------------------|----------------------
//	OK                    | the call did what was asked,     | every handler's
//	                      | including finding nothing        | success path
//	INVALID_ARGUMENT      | the request is malformed or      | errInvalidName,
//	                      | names a reserved name            | errBadLeaseToken,
//	                      |                                  | errStream*
//	NOT_FOUND             | the named queue does not exist   | errNoSuchQueue
//	ALREADY_EXISTS        | a queue of that name is present  | the backend's own
//	                      |                                  | status, passed through
//	FAILED_PRECONDITION   | the queue exists but this engine | errUnreadableQueue
//	                      | cannot read its settings         |
//	ABORTED               | the message is no longer this    | errNotYoursToSettle
//	                      | consumer's to settle             |
//	UNIMPLEMENTED         | the backend does not serve this  | the backend's own
//	UNAVAILABLE           | contract / could not be reached  | status, surfaced
//	INTERNAL              | / failed on its own account      | through spi.Error
//
// Statuses gRPC produces for the transport rather than for an outcome —
// CANCELLED, DEADLINE_EXCEEDED — are outside the table. Nothing here produces
// one, and a stream ending in CANCELLED is ordinary rather than a fault.
//
// Reading a status the backend returned is a different act and stays where the
// call is made: dequeue.go decides what FAILED_PRECONDITION from Delete means,
// engine.go what ALREADY_EXISTS from CreatePartition means. Those are
// interpretations of someone else's answer, not statuses this engine chose.
//
// Three rows are not built here because they are not the engine's to invent:
// ALREADY_EXISTS, UNIMPLEMENTED, UNAVAILABLE and INTERNAL all arrive from the
// backend, and spi.Error carries the backend's own status through to the
// client with the operation and partition named in the message.

// errSettlementRefused is a Delete the backend would not apply.
//
// It covers both refusals, which mean the same thing to the consumer: the
// message is no longer its to settle. FAILED_PRECONDITION means the hold
// lapsed and the message passed to another consumer. NOT_FOUND means it is no
// longer in the queue — the reaper moved it after its attempts ran out, or an
// earlier acknowledgement of the same message already deleted it. Nothing in
// the SPI tells those apart: there is no read by id, so no call the engine
// could make would say which happened, and the engine does not claim to know.
//
// It is an internal sentinel. What a client sees is errNotYoursToSettle.
var errSettlementRefused = errors.New("the message is no longer this consumer's to settle")

// errInvalidName turns a name queue.CheckName refused into a client status.
//
// INVALID_ARGUMENT rather than ALREADY_EXISTS: the request is malformed, and
// it is refused whether or not that partition happens to exist.
func errInvalidName(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

// errNoSuchQueue is a queue absent from storage.
//
// It is never returned because the engine has not heard of a queue — only
// because ListPartitions did not have it. A queue created a moment ago on
// another replica is real, and this status would be a lie about it.
func errNoSuchQueue(name string) error {
	return status.Errorf(codes.NotFound, "queue %q does not exist", name)
}

// errUnreadableQueue is a queue that exists and cannot be served.
//
// FAILED_PRECONDITION: the target exists, but not in the state the caller
// named. The engine does not know the queue's attempt ceiling, so claiming
// would risk delivering past a limit it cannot see. Refusing is forced rather
// than chosen.
func errUnreadableQueue(name string) error {
	return status.Errorf(codes.FailedPrecondition,
		"queue %q has settings this engine cannot read, so it is not served here", name)
}

// errNotYoursToSettle is what a consumer is told when its Delete was refused.
//
// ABORTED, and not FAILED_PRECONDITION, which a client receives for a queue
// whose settings this engine cannot read — an unrelated condition calling for
// an unrelated response. And not OK, which would tell this consumer it had
// settled a message another consumer is at that moment still processing, or
// one sitting in a dead letter queue.
func errNotYoursToSettle(token queue.Token) error {
	return status.Errorf(codes.Aborted,
		"message %q in queue %q was not settled: %s", token.ID, token.Partition, errSettlementRefused)
}

// errBadLeaseToken is a token no engine serving this contract issued. That is
// a malformed request rather than a lost race.
func errBadLeaseToken(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

// The three ways a StreamingDequeue stream can be malformed. Each terminates
// the stream, because a consumer that opened one incorrectly will not recover
// by being told and carrying on.
func errStreamNotOpened() error {
	return status.Error(codes.InvalidArgument,
		"the first message on this stream must be an open naming the queue and the in-flight limit")
}

func errStreamClosedBeforeOpen() error {
	return status.Error(codes.InvalidArgument, "the stream closed before it opened")
}

func errStreamReopened() error {
	return status.Error(codes.InvalidArgument,
		"this stream is already open; every message after the first is an acknowledgement")
}

func errStreamUnknownMessage() error {
	return status.Error(codes.InvalidArgument,
		"the stream carried neither an open nor an acknowledgement")
}

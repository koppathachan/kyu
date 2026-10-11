package engine

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/koppathachan/kyu/internal/queue"
	kyuv1 "github.com/koppathachan/kyu/kyu/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// streamPollInterval is how long a stream with an empty queue and nothing
// outstanding waits before claiming again.
//
// A lapsed hold is not an event and nothing notifies the engine that work has
// arrived, so a stream that has run the queue dry has nothing to wait on but a
// timer. It is short because a consumer that opened a stream is asking to be
// given work promptly.
const streamPollInterval = 100 * time.Millisecond

// releaseGrace is how long the releases at the end of a stream are given. The
// stream's own context is usually already cancelled by then — that is what
// ended the stream — so the releases run on a context of their own.
const releaseGrace = 5 * time.Second

// StreamingDequeue delivers messages continuously over one stream.
//
// Its semantics are identical to Dequeue and Ack in every respect. Where Ack
// would fail with ABORTED, this stream carries a settlement rejection naming
// the message instead.
func (s *Server) StreamingDequeue(stream grpc.BidiStreamingServer[kyuv1.StreamingDequeueRequest, kyuv1.StreamingDequeueResponse]) error {
	ctx := stream.Context()

	open, err := recvOpen(stream)
	if err != nil {
		return err
	}

	name := open.GetQueue()
	if err := queue.CheckName(name); err != nil {
		return errInvalidName(err)
	}
	settings, err := s.settingsFor(ctx, name)
	if err != nil {
		return err
	}

	// At least one, defaulting to one. Every message sent is a message
	// claimed: without a limit one slow consumer attached to a deep queue
	// would claim the whole queue, and every other consumer would see an
	// empty one.
	limit := int(open.GetInFlight())
	if limit < 1 {
		limit = 1
	}

	// held is what this stream has out and unacknowledged, by message id. It
	// is read and written only by this goroutine.
	held := make(map[string]queue.Token, limit)
	defer s.releaseHeld(held)

	incoming := make(chan *kyuv1.StreamingDequeueRequest)
	recvErr := make(chan error, 1)
	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			select {
			case incoming <- req:
			case <-ctx.Done():
				return
			}
		}
	}()

	timer := time.NewTimer(streamPollInterval)
	defer timer.Stop()

	for {
		// Fill the window: claim and send until the limit is reached or the
		// queue runs dry.
		drained := false
		for len(held) < limit {
			item, err := s.claim(ctx, name, settings)
			if err != nil {
				return err
			}
			if item == nil {
				drained = true
				break
			}
			token := queue.Token{Partition: name, ID: item.GetId(), Attempts: item.GetAttempts()}
			held[token.ID] = token
			if err := stream.Send(&kyuv1.StreamingDequeueResponse{
				Response: &kyuv1.StreamingDequeueResponse_Message{Message: messageFrom(name, item)},
			}); err != nil {
				return err
			}
		}

		// Wait for an acknowledgement, or — if the queue was empty — for long
		// enough that claiming again is worth it.
		//
		// When the window is full there is nothing for a timer to do: nothing
		// more is claimed until an acknowledgement frees a slot, however deep
		// the queue is. A nil channel is never ready, so leaving poll unset is
		// how that branch is switched off.
		var poll <-chan time.Time
		if drained {
			// Go 1.23 made timer channels unbuffered, so a Reset cannot leave
			// a stale tick behind for this select to pick up.
			timer.Reset(streamPollInterval)
			poll = timer.C
		}

		select {
		case <-ctx.Done():
			// The consumer cancelled or vanished. Ordinary: what it held is
			// released by the deferred call, and nothing is reported.
			return nil

		case err := <-recvErr:
			if errors.Is(err, io.EOF) {
				return nil // the consumer closed its half cleanly
			}
			if status.Code(err) == codes.Canceled {
				return nil
			}
			return err

		case req := <-incoming:
			if err := s.handleStreamRequest(ctx, stream, held, req); err != nil {
				return err
			}

		case <-poll:
		}
	}
}

// handleStreamRequest applies one message from the consumer.
func (s *Server) handleStreamRequest(
	ctx context.Context,
	stream grpc.BidiStreamingServer[kyuv1.StreamingDequeueRequest, kyuv1.StreamingDequeueResponse],
	held map[string]queue.Token,
	req *kyuv1.StreamingDequeueRequest,
) error {
	switch body := req.GetRequest().(type) {
	case *kyuv1.StreamingDequeueRequest_Open:
		return errStreamReopened()

	case *kyuv1.StreamingDequeueRequest_Ack:
		token, err := queue.DecodeToken(body.Ack.GetLeaseToken())
		if err != nil {
			// A token this engine never issued names no message this stream
			// holds, which is ignored rather than fatal.
			return nil
		}
		return s.settleOnStream(ctx, stream, held, token)

	default:
		return errStreamUnknownMessage()
	}
}

// settleOnStream settles one acknowledgement and frees its slot.
func (s *Server) settleOnStream(
	ctx context.Context,
	stream grpc.BidiStreamingServer[kyuv1.StreamingDequeueRequest, kyuv1.StreamingDequeueResponse],
	held map[string]queue.Token,
	token queue.Token,
) error {
	mine, ok := held[token.ID]
	if !ok || mine != token {
		// An acknowledgement naming a message this consumer does not hold is
		// ignored, and does not end the stream. Its slot is not freed either,
		// because it never occupied one.
		return nil
	}

	// The slot is freed whether the settlement succeeds or is refused. A
	// refused settlement means the message is no longer this stream's, so
	// holding the slot against it would leave a consumer whose holds had all
	// lapsed acknowledging everything, collecting a rejection for each, and
	// then sitting at its limit receiving nothing on a stream that is open.
	delete(held, token.ID)

	switch err := s.settle(ctx, token); {
	case err == nil:
		return nil
	case errors.Is(err, errSettlementRefused):
		// The streaming form of Ack's ABORTED. Staying silent would leave the
		// consumer believing it had settled a message another consumer is at
		// that moment processing, or one sitting in a dead letter queue.
		return stream.Send(&kyuv1.StreamingDequeueResponse{
			Response: &kyuv1.StreamingDequeueResponse_SettlementRejection{
				SettlementRejection: &kyuv1.SettlementRejection{MessageId: token.ID},
			},
		})
	default:
		return err
	}
}

// recvOpen reads the stream's first message, which must be an Open.
func recvOpen(stream grpc.BidiStreamingServer[kyuv1.StreamingDequeueRequest, kyuv1.StreamingDequeueResponse]) (*kyuv1.Open, error) {
	req, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errStreamClosedBeforeOpen()
		}
		return nil, err
	}
	open, ok := req.GetRequest().(*kyuv1.StreamingDequeueRequest_Open)
	if !ok {
		return nil, errStreamNotOpened()
	}
	return open.Open, nil
}

// releaseHeld returns everything a stream held and had not acknowledged, at
// once, rather than waiting out each hold.
//
// The stream's context is usually already cancelled by the time this runs, so
// the releases are given a context of their own. Each names the hold it
// clears, using the attempt count from its claim; a refusal means the hold had
// already lapsed and passed on, which is ordinary.
func (s *Server) releaseHeld(held map[string]queue.Token) {
	if len(held) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseGrace)
	defer cancel()

	for _, token := range held {
		if err := s.release(ctx, token); err != nil {
			s.log.Error("a message held by a stream that ended could not be released; its hold will lapse on its own",
				"queue", token.Partition, "id", token.ID, "error", err)
		}
	}
}

package engine

import (
	"context"
	"errors"

	"github.com/koppathachan/kyu/internal/queue"
	storagev1 "github.com/koppathachan/kyu/kyu/storage/v1"
	kyuv1 "github.com/koppathachan/kyu/kyu/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Dequeue returns at most one message, held for the queue's hold duration.
func (s *Server) Dequeue(ctx context.Context, req *kyuv1.DequeueRequest) (*kyuv1.DequeueResponse, error) {
	name := req.GetQueue()
	if err := queue.CheckName(name); err != nil {
		return nil, errInvalidName(err)
	}
	settings, err := s.settingsFor(ctx, name)
	if err != nil {
		return nil, err
	}

	item, err := s.claim(ctx, name, settings)
	if err != nil {
		return nil, err
	}
	if item == nil {
		// An empty queue is the ordinary state of most queues most of the
		// time. It is a success carrying no message, never an error.
		return &kyuv1.DequeueResponse{}, nil
	}
	return &kyuv1.DequeueResponse{Message: messageFrom(name, item)}, nil
}

// Ack settles a message and removes it from its queue.
func (s *Server) Ack(ctx context.Context, req *kyuv1.AckRequest) (*kyuv1.AckResponse, error) {
	token, err := queue.DecodeToken(req.GetLeaseToken())
	if err != nil {
		return nil, errBadLeaseToken(err)
	}

	switch err := s.settle(ctx, token); {
	case err == nil:
		return &kyuv1.AckResponse{}, nil
	case errors.Is(err, errSettlementRefused):
		// Both refusals are ordinary for the engine and are not reported as
		// faults. Both are reported to the consumer, which asked for
		// something that did not happen.
		return nil, errNotYoursToSettle(token)
	default:
		return nil, err
	}
}

// claim takes one message under a hold.
//
// The hold duration is the queue's setting and there is no per-dequeue
// override: the two dequeue forms must observe identical semantics, and an
// override added to the unary form only would break that.
//
// The instant and the hold are both computed here, from the engine's clock,
// and passed as arguments. The ceiling goes with them, so that a message that
// has spent its attempts is undeliverable at the backend rather than by the
// engine remembering to check — which is what lets the reaper be an
// optimisation rather than part of the correctness path.
func (s *Server) claim(ctx context.Context, name string, settings queue.Settings) (*storagev1.Item, error) {
	now := s.now()
	return s.storage.Claim(ctx, name,
		now.UnixNano(),
		now.Add(settings.HoldDuration).UnixNano(),
		settings.AttemptCeiling,
	)
}

// settle issues the engine's only Delete.
//
// Nothing else in the engine deletes: dead-lettering moves, a broken stream
// releases, and an unacknowledged message is left where it is. A message
// leaves a queue when, and only when, a consumer acknowledges it.
//
// The Delete names the hold it settles, using the attempt count the claim
// returned, which travels in the lease token. Neither refusal is retried: the
// condition that refused it will not change.
func (s *Server) settle(ctx context.Context, token queue.Token) error {
	err := s.storage.Delete(ctx, token.Partition, token.ID, token.Attempts)
	switch status.Code(err) {
	case codes.OK:
		return nil
	case codes.FailedPrecondition, codes.NotFound:
		return errSettlementRefused
	default:
		return err
	}
}

// release returns a held message at once, naming the hold it clears.
//
// A release refused with FAILED_PRECONDITION means the hold had already lapsed
// and passed to another consumer, and NOT_FOUND that the message has left the
// queue. Both are ordinary and neither is retried.
func (s *Server) release(ctx context.Context, token queue.Token) error {
	err := s.storage.Release(ctx, token.Partition, token.ID, token.Attempts)
	switch status.Code(err) {
	case codes.OK, codes.FailedPrecondition, codes.NotFound:
		return nil
	default:
		return err
	}
}

// messageFrom renders a claimed item as a delivery.
func messageFrom(name string, it *storagev1.Item) *kyuv1.Message {
	return &kyuv1.Message{
		Id:      it.GetId(),
		Payload: it.GetValue(),
		Attempt: it.GetAttempts(),
		LeaseToken: queue.Token{
			Partition: name,
			ID:        it.GetId(),
			Attempts:  it.GetAttempts(),
		}.Encode(),
	}
}

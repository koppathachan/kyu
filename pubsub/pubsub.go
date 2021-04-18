package pubsub

import (
	"context"

	"github.com/sasidakh/kyu/pubsub/q"
	"github.com/sasidakh/kyu/q/msg"
)

type Server struct {
	UnimplementedPubSubServer
	Qaddr string
}

func (s Server) Publish(ctx context.Context, m *msg.Message) (*PublishResult, error) {
	wr, err := q.Publish(s.Qaddr, m)
	if err != nil {
		return nil, err
	}
	return &PublishResult{
		Ack: wr.Ack,
	}, nil
}

func (s Server) Subscribe(qu *msg.Queue, ss PubSub_SubscribeServer) error {
	ch, err := q.SubscribeChannel(s.Qaddr, qu)
	if err != nil {
		return err
	}
	for {
		select {
		case m := <-ch:
			if err := ss.Send(m); err != nil {
				return err
			}
		}
	}
}

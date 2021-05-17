package q

import (
	"context"
	"log"

	"github.com/sasidakh/kyu/q/msg"
	"github.com/sasidakh/kyu/q/store"
)

type Server struct {
	UnimplementedQServer
	s store.Store
}

func New(s store.Store) Server {
	return Server{s: s}
}

func (s Server) Create(ctx context.Context, m *msg.Queue) (*CreateResponse, error) {
	if err := s.s.Create(ctx, m.Name, 256*20); err != nil {
		return nil, err
	}
	return &CreateResponse{
		Ack: &msg.Ack{
			Q: &msg.Queue{
				Name: m.Name,
			},
			Ok: true,
		},
	}, nil
}

func (s Server) Enqueue(ctx context.Context, m *msg.Message) (*WriteResult, error) {
	wr, err := s.s.Insert(ctx, store.Message{
		Data: m.Data,
		Q: store.Q{
			Name: m.Q.Name,
		},
	})
	if err != nil {
		return nil, err
	}
	return &WriteResult{
		Q:     m.Q,
		Id:    wr.ID,
		Count: uint32(len(m.Data)),
	}, nil
}

func (s Server) Dequeue(qu *msg.Queue, qs Q_DequeueServer) error {
	ch, err := s.s.Tail(qu.Name)
	if err != nil {
		return err
	}
	for {
		select {
		case m := <-ch:
			if err := qs.Send(&msg.Message{
				Q:    qu,
				Id:   m.ID.Hex(),
				Data: m.Data,
			}); err != nil {
				// TODO: retry
				log.Println(err)
			} else {
				// success mark as read
				go s.s.MarkRead(context.TODO(), m)
			}
		}
	}
}

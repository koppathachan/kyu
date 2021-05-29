package q

import (
	"context"
	"fmt"
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

func (s Server) Create(ctx context.Context, mq *msg.Queue) (*CreateResponse, error) {
	if err := s.s.Create(ctx, store.CreateOptions{
		Size: 1073741824,
		Name: mq.Name,
	}); err != nil {
		return nil, err
	}
	return &CreateResponse{
		Ack: &msg.Ack{
			Q: &msg.Queue{
				Name: mq.Name,
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
				log.Println("Error sending after dequeue", err)
			} else {
				// success mark as read
				fmt.Println("MArkREAD  :  ", m)
				go s.s.MarkRead(context.TODO(), m)
			}
		}
	}
}

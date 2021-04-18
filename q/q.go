package q

import (
	"context"
	"errors"
	"fmt"

	"github.com/sasidakh/kyu/q/msg"
)

type Server struct {
	UnimplementedQServer
}

type queue struct {
	name  string
	items []string
}

func (qu *queue) Enqueue(rm string) {
	qu.items = append(qu.items, rm)
}

func (qu *queue) Dequeue() string {
	item := qu.items[0]
	qu.items = qu.items[1:]
	return item
}

var qmap map[string]*queue = make(map[string]*queue)

func (s Server) Create(ctx context.Context, que *msg.Queue) (*msg.Ack, error) {
	_, ok := qmap[que.Name]
	if ok {
		return &msg.Ack{
			Q:       que,
			Ok:      ok,
			Message: "Exists",
		}, nil
	}
	qmap[que.Name] = &queue{
		name:  que.Name,
		items: []string{},
	}
	return &msg.Ack{
		Q:       que,
		Ok:      true,
		Message: "Created",
	}, nil
}

func writeRes(qname string, len uint32) *WriteResult {
	return &WriteResult{
		Ack: &msg.Ack{
			Q: &msg.Queue{
				Name: qname,
			},
			Ok:      true,
			Message: "SUCCESS",
		},
		Count: len,
	}
}

func (s Server) Enqueue(ctx context.Context, m *msg.Message) (*WriteResult, error) {
	qu, ok := qmap[m.Q.Name]
	if !ok {
		return nil, errors.New("NoQ")
	}
	qu.Enqueue(m.Data)
	return writeRes(m.Q.Name, uint32(len(m.Data))), nil
}

func (s Server) Dequeue(q *msg.Queue, qs Q_DequeueServer) error {
	qu, ok := qmap[q.Name]
	if !ok {
		return errors.New("NoQ")
	}
	waitc := make(chan msg.Message)
	go func() {
		for {
			if len(qu.items) != 0 {
				fmt.Println("sending message", qu)
				if err := qs.Send(&msg.Message{
					Q:    q,
					Data: qu.items[0],
				}); err == nil {
					fmt.Println("dequeing")
					qu.Dequeue()
				}
			}
		}
	}()
	<-waitc
	return nil
}

package q

import (
	"context"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	UnimplementedQueueServer
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

func (s Server) Create(ctx context.Context, qd *QueueDetails) (*CreateResponse, error) {
	qu, ok := qmap[qd.Qname]
	if ok {
		return &CreateResponse{
			Qname: qu.name,
			Ack: &Ack{
				Ok:      ok,
				Message: "Queue Exists",
			},
		}, nil
	}
	qmap[qd.Qname] = &queue{
		name:  qd.Qname,
		items: []string{},
	}
	return &CreateResponse{
		Qname: qd.Qname,
		Ack: &Ack{
			Ok:      true,
			Message: "Queue Created",
		},
	}, nil
}

func (s Server) Enqueue(ctx context.Context, m *Message) (*WriteResult, error) {
	qu, ok := qmap[m.Qname]
	if !ok {
		return nil, status.Error(codes.NotFound, "Queue not found")
	}
	qu.Enqueue(m.Data)
	return &WriteResult{
		Qname: qu.name,
		Ack: &Ack{
			Ok:      ok,
			Message: "q'ed",
		},
		Count: uint32(len([]byte(m.Data))),
	}, nil
}

func (s Server) Dequeue(ctx context.Context, qd *QueueDetails) (*Message, error) {
	qu, ok := qmap[qd.Qname]
	if !ok {
		return nil, status.Error(codes.NotFound, "Queue not found")
	}
	if len(qu.items) == 0 {
		return nil, io.EOF
	}
	item := qu.Dequeue()
	return &Message{
		Qname: qu.name,
		Data:  item,
	}, nil
}

package pubsub

import (
	"context"
	"io"
	"time"

	"log"

	"github.com/sasidakh/kyu/q"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type Server struct {
	UnimplementedPublisherServer
	Qaddr string
}

func makeQMsg(m *Message) (*q.Message, error) {
	qm := new(q.Message)
	bytes, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(bytes, qm); err != nil {
		return nil, err
	}
	return qm, nil
}

func (s Server) Publish(ctx context.Context, m *Message) (*PublishResult, error) {
	conn, err := grpc.Dial(s.Qaddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := q.NewQueueClient(conn)
	qm, err := makeQMsg(m)
	if err != nil {
		return nil, err
	}
	wr, err := c.Enqueue(ctx, qm)
	if err != nil {
		return nil, err
	}
	return &PublishResult{
		Qname: wr.Qname,
		Ack: &Ack{
			Ok:      wr.Ack.Ok,
			Message: wr.Ack.Message,
		},
	}, nil
}

func makeMsg(qm *q.Message) (*Message, error) {
	bytes, err := proto.Marshal(qm)
	if err != nil {
		return nil, err
	}
	m := new(Message)
	if err := proto.Unmarshal(bytes, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s Server) Subscribe(qu *Queue, ss Publisher_SubscribeServer) error {
	conn, err := grpc.Dial(s.Qaddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := q.NewQueueClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	qd := &q.QueueDetails{Qname: qu.Qname}

	estream, err := c.Events(ctx, qd)

	for {
		qe, err := estream.Recv()
		if err != nil {
			return err
		}
		if qe.Event == q.Event_DEQUEUE {
			qm, err := c.Dequeue(ctx, qd)
			if err != nil {
				return err
			}
			m, err := makeMsg(qm)
			if err != nil {
				return err
			}
			if err := ss.Send(m); err != nil {
				return status.Error(status.Code(err), err.Error())
			}
		}
	}
}

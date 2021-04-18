package pubsub

import (
	"context"
	"time"

	"log"

	"github.com/sasidakh/kyu/q"
	"github.com/sasidakh/kyu/q/msg"
	"google.golang.org/grpc"
)

type Server struct {
	UnimplementedPubSubServer
	Qaddr string
}

func (s Server) Publish(ctx context.Context, m *msg.Message) (*PublishResult, error) {
	conn, err := grpc.Dial(s.Qaddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := q.NewQClient(conn)
	if err != nil {
		return nil, err
	}
	wr, err := c.Enqueue(ctx, m)
	return &PublishResult{
		Ack: wr.Ack,
	}, nil
}

func (s Server) Subscribe(qu *msg.Queue, ss PubSub_SubscribeServer) error {
	conn, err := grpc.Dial(s.Qaddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := q.NewQClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	dqstream, err := c.Dequeue(ctx, qu)

	for {
		m, _ := dqstream.Recv()
		ss.Send(m)
	}
}

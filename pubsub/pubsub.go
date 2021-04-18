package pubsub

import (
	"context"
	"fmt"
	"io"

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
	if err != nil {
		return nil, err
	}
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
	// ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	// defer cancel()
	ack, err := c.Create(context.TODO(), qu)
	if err != nil {
		return err
	}
	fmt.Println(ack)
	dqstream, err := c.Dequeue(context.TODO(), qu)
	if err != nil {
		return err
	}
	waitc := make(chan msg.Message)
	go func() {
		for {
			m, err := dqstream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			}
			if err != nil {
				fmt.Println("ERERRER", err)
			}
			if m != nil {
				ss.Send(m)
			}
		}
	}()
	<-waitc
	return nil
}

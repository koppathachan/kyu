package sub

import (
	"context"
	"log"
	"time"

	"github.com/sasidakh/kyu/q"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type Server struct {
	UnimplementedSubscriberServer
}

const (
	address = "localhost:50051"
)

func (s *Server) Subscribe(qu *Queue, ss Subscriber_SubscribeServer) error {
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := q.NewQueueClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	qm, err := c.Dequeue(ctx, &q.QueueDetails{
		Qname: qu.Qname,
	})
	bytes, err := proto.Marshal(qm)
	if err != nil {
		return err
	}
	m := new(Message)
	if err := proto.Unmarshal(bytes, m); err != nil {
		return err
	}
	if err != nil {
		return status.Error(status.Code(err), err.Error())
	}

	if err := ss.Send(m); err != nil {
		return status.Error(status.Code(err), err.Error())
	}
	return nil
}

package q

import (
	"context"
	"io"
	"log"

	"github.com/sasidakh/kyu/q"
	"github.com/sasidakh/kyu/q/msg"
	"google.golang.org/grpc"
)

var conn *grpc.ClientConn

func getConn(addr string) *grpc.ClientConn {
	var err error
	if conn == nil {
		log.Println("connection to ", addr)
		conn, err = grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			log.Panicln(err)
		}
	}
	return conn
}

func Publish(addr string, m *msg.Message) (*q.WriteResult, error) {
	conn := getConn(addr)
	c := q.NewQClient(conn)
	wr, err := c.Enqueue(context.TODO(), m)
	if err != nil {
		return nil, err
	}
	return wr, nil
}

func SubscribeChannel(addr string, qu *msg.Queue) (<-chan *msg.Message, error) {
	out := make(chan *msg.Message)
	conn = getConn(addr)
	c := q.NewQClient(conn)
	_, err := c.Create(context.TODO(), qu)
	if err != nil {
		return nil, err
	}
	qc, err := c.Dequeue(context.TODO(), qu)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			m, err := qc.Recv()
			if err == io.EOF {
				close(out)
				break
			}
			if err != nil {
				log.Println(err)
			}
			if m != nil {
				out <- m
			}
		}
	}()
	return out, nil
}

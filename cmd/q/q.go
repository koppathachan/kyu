package main

import (
	"log"

	"fmt"
	"net"

	"github.com/sasidakh/kyu/q"
	"google.golang.org/grpc"
)

func main() {
	listner, err := net.Listen("tcp", fmt.Sprintf("localhost:%s", "50051"))
	if err != nil {
		log.Fatalln(err)
	}
	s := grpc.NewServer()
	q.RegisterQueueServer(s, q.Server{})
	log.Println("Queue at : ", listner.Addr())
	if err := s.Serve(listner); err != nil {
		log.Fatalln(err)
	}
}

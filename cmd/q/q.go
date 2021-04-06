package main

import (
	"fmt"
	"log"

	"fmt"
	"net"

	"github.com/sasidakh/kyu/q"
	"google.golang.org/grpc"
)

func main() {
	listner, err := net.Listen("tcp", fmt.Sprintf("localhost:%s", "3000"))
	if err != nil {
		log.Fatalln(err)
	}
	s := grpc.NewServer()
	q.RegisterQueueServer(s, q.Server{})
	if err := s.Serve(listner); err != nil {
		log.Fatalln(err)
	}
}

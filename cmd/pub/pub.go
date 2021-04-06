package main

import (
	"fmt"
	"log"
	"net"

	"github.com/sasidakh/kyu/pub"
	"google.golang.org/grpc"
)

func main() {
	listner, err := net.Listen("tcp", fmt.Sprintf("localhost:%s", "3000"))
	if err != nil {
		log.Fatalln(err)
	}
	s := grpc.NewServer()
	pub.RegisterPublisherServer(s, pub.Server{})
	if err := s.Serve(listner); err != nil {
		log.Fatalln(err)
	}
}

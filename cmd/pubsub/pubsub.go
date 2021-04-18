package main

import (
	"fmt"
	"log"
	"net"

	"github.com/sasidakh/kyu/pubsub"
	"google.golang.org/grpc"
)

func main() {
	listner, err := net.Listen("tcp", fmt.Sprintf("localhost:%s", "50050"))
	if err != nil {
		log.Fatalln(err)
	}
	s := grpc.NewServer()
	pubsub.RegisterPubSubServer(s, pubsub.Server{
		Qaddr: "localhost:50051",
	})
	log.Println("Pubsub at : ", listner.Addr())
	if err := s.Serve(listner); err != nil {
		log.Fatalln(err)
	}
}

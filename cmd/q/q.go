package main

import (
	"context"
	"log"

	"fmt"
	"net"

	"github.com/sasidakh/kyu/q"
	"github.com/sasidakh/kyu/q/store"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
)

func main() {
	listner, err := net.Listen("tcp", fmt.Sprintf("localhost:%s", "50051"))
	if err != nil {
		log.Fatalln(err)
	}
	s := grpc.NewServer()
	client, err := mongo.NewClient(
		options.Client().ApplyURI(
			"mongodb://localhost:27017,localhost:27018,localhost:27019/queue?replicaSet=mongodb-replicaset",
		),
	)
	if err != nil {
		log.Panicln(err)
	}
	if err := client.Connect(context.Background()); err != nil {
		log.Panicln(err)
	}
	log.Println("Connected to database")
	st := store.New(client, "queue", 1)
	q.RegisterQServer(s, q.New(st))
	log.Println("Queue at : ", listner.Addr())
	if err := s.Serve(listner); err != nil {
		log.Fatalln(err)
	}
}

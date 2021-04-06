package main

import (
	"fmt"
	"log"

	"context"
	"time"

	"github.com/sasidakh/kyu/pkg/q"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type db struct {
	cl *mongo.Collection
}

func (d db) Write(p []byte) (int, error) {
	var doc bson.M = make(bson.M)
	_, err := d.cl.InsertOne(context.TODO(), doc)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func (d db) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

func (d db) Offset() int64 {
	return 0
}

func newStore() q.Store {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:30002/test"))
	if err != nil {
		log.Fatalln(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err = client.Connect(ctx); err != nil {
		cancel()
		log.Fatalln(err)
	}
	coll := client.Database("test").Collection("test")
	return db{cl: coll}
}

func main() {
	tq := q.New("test")
	_, err := tq.Front()
	fmt.Println(err)
	_, err = tq.Dequeue()
	fmt.Println(err)
	tq.Enqueue(q.RawMessage("a"))
	tq.Enqueue(q.RawMessage("b"))
	tq.Enqueue(q.RawMessage("c"))
	tq.Enqueue(q.RawMessage("d"))
	// fmt.Println(tq.Dequeue())
	// fmt.Println(tq.Dequeue())
	fmt.Println(tq.Dequeue())
	fmt.Println(tq.Dequeue())
	// tq.Dequeue()
	// tq.Dequeue()
	fmt.Printf("%v", tq)
}

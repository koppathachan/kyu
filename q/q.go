package q

import (
	"context"
	"fmt"
	"log"

	"github.com/sasidakh/kyu/q/msg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Server struct {
	UnimplementedQServer
}

type queue struct {
	name  string
	items []string
}

func (qu *queue) Enqueue(rm string) {
	qu.items = append(qu.items, rm)
}

func (qu *queue) Dequeue() string {
	item := qu.items[0]
	qu.items = qu.items[1:]
	return item
}

var qmap map[string]*queue = make(map[string]*queue)

var client *mongo.Client

func getClient() *mongo.Client {
	var err error
	if client == nil {
		client, err = mongo.NewClient(
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
	}
	return client
}

func (s Server) Create(ctx context.Context, que *msg.Queue) (*msg.Ack, error) {
	cl := getClient()
	col := cl.Database("queue").Collection(que.Name)
	fmt.Println("Creating queue", col.Name())
	iname, err := col.Indexes().CreateOne(context.TODO(), mongo.IndexModel{
		Keys:    bson.M{"Id": -1},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}
	return &msg.Ack{
		Q:       que,
		Ok:      true,
		Message: iname,
	}, nil
}

func writeRes(qname string, len uint32) *WriteResult {
	return &WriteResult{
		Ack: &msg.Ack{
			Q: &msg.Queue{
				Name: qname,
			},
			Ok:      true,
			Message: "SUCCESS",
		},
		Count: len,
	}
}

func (s Server) Enqueue(ctx context.Context, m *msg.Message) (*WriteResult, error) {
	fmt.Println("enquququququ ", m)
	cl := getClient()
	col := cl.Database("queue").Collection(m.Q.Name)
	res, err := col.InsertOne(context.TODO(), bson.D{
		{Key: "Id", Value: m.Id},
		{Key: "Data", Value: m.Data},
	})
	fmt.Println(res)
	if err != nil {
		return nil, err
	}
	return writeRes(m.Q.Name, uint32(len(m.Data))), nil
}

func (s Server) Dequeue(q *msg.Queue, qs Q_DequeueServer) error {
	cl := getClient()
	col := cl.Database("queue").Collection(q.Name)
	stream, err := col.Watch(context.TODO(), mongo.Pipeline{})
	if err != nil {
		return nil
	}
	defer stream.Close(context.TODO())
	for stream.Next(context.TODO()) {
		var m msg.Message
		if err := stream.Decode(&m); err != nil {
			log.Panicln(err)
		}
		if err := qs.Send(&m); err != nil {
			log.Panicln(err)
		}
	}
	return nil
}

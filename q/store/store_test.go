package store_test

import (
	"fmt"
	"log"
	"os"
	"testing"

	"context"

	"github.com/sasidakh/kyu/q/store"
	"github.com/sasidakh/pkg/testutils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const dbName = "queue"
const collection = "testq"

func must(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

var client, err = mongo.NewClient(
	options.Client().ApplyURI(
		"mongodb://localhost:27017,localhost:27018,localhost:27019/queue?replicaSet=mongodb-replicaset",
	),
)

func setup() {
	must(client.Connect(context.Background()))
}

func teardown() {
	must(client.Database(dbName).Collection(collection).Drop(context.Background()))
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func TestTail(t *testing.T) {
	ok := testutils.Okayer(t)
	s := store.New(client, dbName, 1)
	oid := primitive.NewObjectID()
	m := store.Message{
		ID:        &oid,
		Data:      "aaaa",
		Q:         store.Q{Name: collection},
		ReadCount: 1,
	}
	ok(s.Create(context.Background(), store.CreateOptions{
		Size: 10000,
		Name: collection,
	}))
	_, err := s.Insert(context.Background(), m)
	ok(err)
	ch, err := s.Tail(collection)
	ok(err)
	_, err = s.MarkRead(context.Background(), <-ch)
	ok(err)
	ch2, err := s.Tail(collection)
	fmt.Println(<-ch2)
}

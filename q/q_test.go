package q_test

import (
	"context"
	"log"
	"os"
	"strconv"
	"testing"

	"net"

	"github.com/sasidakh/kyu/q"
	"github.com/sasidakh/kyu/q/msg"
	"github.com/sasidakh/kyu/q/store"
	"github.com/sasidakh/pkg/testutils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
)

const address = "localhost:50051"
const dbName = "queue"
const collection = "testq"

// var wg = new(sync.WaitGroup)
var client *mongo.Client
var conn *grpc.ClientConn
var qclient q.QClient

func must(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func startServer(st store.Store) {
	l, err := net.Listen("tcp", address)
	must(err)
	s := grpc.NewServer()
	q.RegisterQServer(s, q.New(st))
	go func() {
		must(s.Serve(l))
	}()
}

func setup() {
	var err error
	client, err = mongo.NewClient(
		options.Client().ApplyURI(
			"mongodb://localhost:27017,localhost:27018,localhost:27019/queue?replicaSet=mongodb-replicaset",
		),
	)
	must(err)
	must(client.Connect(context.Background()))
	s := store.New(client, dbName, 1)
	startServer(s)
	conn, err = grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	must(err)
	qclient = q.NewQClient(conn)
}

func teardown() {
	must(client.Database(dbName).Collection(collection).Drop(context.Background()))
	must(conn.Close())
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func TestCreateEnqueueDequeue(t *testing.T) {
	ok := testutils.Okayer(t)
	assert := testutils.Asserter(t)
	equals := testutils.Equater(t)
	var err error

	mq := msg.Queue{Name: collection}
	cr, err := qclient.Create(context.Background(), &mq)
	ok(err)
	assert(cr.Ack.Ok, "false acknoledgement")
	m := msg.Message{
		Q:    &mq,
		Data: "something enqueued",
	}
	wr, err := qclient.Enqueue(context.Background(), &m)
	ok(err)
	assert(wr.Q.Name == mq.Name, "Wrote to wrong queue")
	dqs, err := qclient.Dequeue(context.Background(), &mq)
	ok(err)
	rm, err := dqs.Recv()
	ok(err)
	equals(m.Data, rm.Data)
}

func TestEnqueueDequeue2(t *testing.T) {
	ok := testutils.Okayer(t)
	// assert := testutils.Asserter(t)
	equals := testutils.Equater(t)
	var err error
	mq := msg.Queue{Name: collection}
	// for i := 0; i < 2; i++ {
	// 	wr, err := qclient.Enqueue(context.Background(), &msg.Message{
	// 		Q:    &mq,
	// 		Data: strconv.Itoa(i),
	// 	})
	// 	ok(err)
	// 	assert(wr.Q.Name == mq.Name, "Wrote to wrong queue")
	// }
	dqs, err := qclient.Dequeue(context.Background(), &mq)
	ok(err)
	for i := 0; i < 2; i++ {
		m, err := dqs.Recv()
		ok(err)
		equals(m.Data, strconv.Itoa(i))
	}
}

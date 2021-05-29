package store

import (
	"context"
	"log"
	"math"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const readCountZero = 1

type Q struct {
	Name string `json:"name"`
}

type Message struct {
	ID        *primitive.ObjectID `json:"_id" bson:"_id"`
	Data      string              `bson:"data"`
	Q         Q                   `bson:"q"`
	ReadCount uint64              `bson:"readCount"`
}

type WriteResult struct {
	Q  Q
	ID string
}

type ReadResult struct {
	Q     Q
	Count int64
	ID    string
}

type CreateOptions struct {
	Size int64
	Name string
}

type Store interface {
	Insert(ctx context.Context, m Message) (*WriteResult, error)
	Tail(name string) (chan Message, error)
	MarkRead(ctx context.Context, m Message) (*ReadResult, error)
	Create(ctx context.Context, opts CreateOptions) error
}

type store struct {
	db        *mongo.Database
	readCount uint64
}

// TODO: markread and find should be atomic
func (s store) MarkRead(ctx context.Context, m Message) (*ReadResult, error) {
	res, err := s.db.Collection(m.Q.Name).UpdateByID(ctx, m.ID, bson.D{{
		Key: "$inc",
		Value: bson.D{{
			Key:   "readCount",
			Value: 1,
		}},
	}})
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return &ReadResult{
		Q:     Q{Name: m.Q.Name},
		Count: res.ModifiedCount + res.UpsertedCount,
		ID:    m.ID.Hex(),
	}, nil
}

func (s store) Insert(ctx context.Context, m Message) (*WriteResult, error) {
	oid := primitive.NewObjectID()
	m.ID = &oid
	m.ReadCount = readCountZero
	res, err := s.db.Collection(m.Q.Name).InsertOne(ctx, m)
	if err != nil {
		return nil, err
	}
	return &WriteResult{
		ID: res.InsertedID.(primitive.ObjectID).Hex(),
		Q:  m.Q,
	}, nil
}

func (s store) Create(ctx context.Context, co CreateOptions) error {
	opts := options.CreateCollection().SetCapped(true).SetSizeInBytes(co.Size)
	if err := s.db.CreateCollection(ctx, co.Name, opts); err != nil {
		return err
	}
	oid := primitive.NewObjectID()
	if _, err := s.db.Collection(co.Name).InsertOne(ctx, Message{
		ID:        &oid,
		Data:      "",
		Q:         Q{Name: co.Name},
		ReadCount: math.MaxInt64,
	}); err != nil {
		return err
	}
	return nil
}

func tailChannel(cur *mongo.Cursor) chan Message {
	ch := make(chan Message)
	go func() {
		defer cur.Close(context.TODO())
		for cur.Next(context.TODO()) {
			var m Message
			if err := cur.Decode(&m); err != nil {
				// TODO: handle recover
				log.Panicln(err)
			}
			ch <- m
		}
	}()
	return ch
}

func (s store) Tail(name string) (chan Message, error) {
	opts := options.Find().SetCursorType(options.Tailable)
	cur, err := s.db.Collection(name).Find(context.TODO(), bson.D{{
		Key: "readCount",
		Value: bson.D{{
			Key:   "$lt",
			Value: s.readCount + readCountZero,
		}},
	}}, opts)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return tailChannel(cur), nil
}

func New(c *mongo.Client, name string, readCount uint64) Store {
	db := c.Database(name)
	return store{db: db, readCount: readCount}
}

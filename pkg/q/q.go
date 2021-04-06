package q

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

type RawMessage []byte

func (r RawMessage) Len() int64 {
	return int64(len(r))
}

type Q interface {
	Front() (RawMessage, error)
	Enqueue(RawMessage) bool
	Dequeue() (RawMessage, error)
}

type Store interface {
	io.WriteSeeker
	Offset() int64
}

var EOQ = errors.New("EOQ")
var EMPTY = errors.New("EMPTY")

type qu struct {
	items []RawMessage
	store Store
}

func (q qu) Front() (RawMessage, error) {
	if len(q.items) == 0 {
		return nil, EOQ
	}
	return q.items[0], nil
}

func (q *qu) Enqueue(r RawMessage) bool {
	_, err := q.store.Seek(0, io.SeekEnd)
	if err != nil {
		panic(err)
	}
	_, err = q.store.Write(r)
	if err != nil {
		panic(err)
	}
	q.items = append(q.items, r)
	return true
}

func (q *qu) Dequeue() (RawMessage, error) {
	if len(q.items) == 0 {
		return nil, EMPTY
	}
	f, _ := q.Front()
	q.store.Seek(q.store.Offset()+f.Len(), io.SeekStart)
	bytes := make([]byte, f.Len())
	q.store.Write(bytes)
	q.items = q.items[1:]
	return f, nil
}

type store struct {
	f *os.File
	o int64
}

func (s *store) Write(p []byte) (int, error) {
	return s.f.Write(p)
}

func (s *store) Seek(offset int64, whence int) (int64, error) {
	return s.f.Seek(offset, whence)
}

func (s store) Offset() int64 {
	return s.o
}

func New(name string) Q {
	f, err := ioutil.TempFile("", name)
	if err != nil {
		panic(err)
	}
	return &qu{
		items: []RawMessage{},
		store: &store{
			f: f,
			o: 0,
		},
	}
}

func StorageQ(s Store) Q {
	return &qu{store: s}
}

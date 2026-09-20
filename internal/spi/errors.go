// Package spi is the engine's only reach into durable state.
//
// It wraps the generated kyu.storage.v1.Storage stubs and holds a connection
// and nothing else: no cache, no backend-specific option, and no branch on
// which implementation answers. A single side channel, however small, converts
// a portable engine into one that works properly against the backend it was
// developed against.
//
// There is no retry anywhere in it. An outcome the backend refused will be
// refused again, and a backend that is down is not a queue in a degraded
// state — retrying would convert one failure into a stream of them and delay
// an operator learning that anything is wrong.
//
// Nothing here names a queue concept. This package speaks partitions and
// items, as the contract does.
package spi

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Error is a failure that came back from the storage server.
//
// The storage implementation is written by the deployment, so when something
// breaks the first question is which side it broke on. An error that does not
// say costs an afternoon. Every SPI-originated failure the engine reports
// therefore names the operation, the partition and the backend's own status.
type Error struct {
	// Backend is the storage target, so that a deployment running more than
	// one knows which answered.
	Backend string

	// Op is the SPI method: "Claim", "Move", and so on.
	Op string

	// Partition is the partition involved, empty only for ListPartitions,
	// which names none.
	Partition string

	// Err is what the call returned, preserved whole so that the backend's
	// own status survives being reported.
	Err error
}

func (e *Error) Error() string {
	where := e.Partition
	if where == "" {
		where = "(no partition)"
	}
	return fmt.Sprintf("storage %s: %s on %s: %s: %s",
		e.Backend, e.Op, where, e.Code(), status.Convert(e.Err).Message())
}

func (e *Error) Unwrap() error { return e.Err }

// Code is the status the backend returned.
func (e *Error) Code() codes.Code { return status.Code(e.Err) }

// GRPCStatus makes the backend's status the one a client receives, with the
// attribution carried in the message. A backend that is down produces
// UNAVAILABLE at the client too: the engine surfaces the failure rather than
// translating it into something of its own, and does not retry it.
func (e *Error) GRPCStatus() *status.Status {
	return status.New(e.Code(), e.Error())
}

// wrap returns an Error for a failed call, or nil if there was none.
func wrap(backend, op, partition string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Backend: backend, Op: op, Partition: partition, Err: err}
}

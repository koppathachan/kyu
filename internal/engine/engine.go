// Package engine implements kyu's queue semantics.
//
// Every queue concept lives here: ordering, holds, attempt counting, dead
// letter transfer. The engine reaches durable state through
// kyu.storage.v1.Storage and through nothing else — no connection, handle or
// configuration belonging to a storage implementation, and no branch anywhere
// on which backend it is talking to.
//
// None of that is implemented yet. This is the service skeleton: it registers,
// it serves, and every method returns UNIMPLEMENTED until the tasks in
// openspec/changes/build-queue-engine fill them in.
package engine

import kyuv1 "github.com/sasidakh/kyu/gen/kyu/v1"

// Server serves kyu.v1.Kyu.
type Server struct {
	// Embedding the generated unimplemented server is what makes an
	// unfinished method return UNIMPLEMENTED rather than fail to compile, so
	// methods can be filled in one at a time.
	kyuv1.UnimplementedKyuServer
}

// New returns an engine that serves nothing yet.
func New() *Server {
	return &Server{}
}

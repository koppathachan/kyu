package q

import (
	context "context"

	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type Server struct {
	UnimplementedQueueServer
}

func (s *Server) Create(ctx context.Context, qd *QueueDetails) (*CreateResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Create not implemented")
}
func (s *Server) Enqueue(ctx context.Context, m *Message) (*WriteResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Enqueue not implemented")
}
func (s *Server) Dequeue(ctx context.Context, qd *QueueDetails) (*Message, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Dequeue not implemented")
}

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package q

import (
	context "context"
	msg "github.com/sasidakh/q/msg"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// QueueClient is the client API for Queue service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type QueueClient interface {
	Create(ctx context.Context, in *msg.Queue, opts ...grpc.CallOption) (*msg.Ack, error)
	Enqueue(ctx context.Context, opts ...grpc.CallOption) (Queue_EnqueueClient, error)
	Dequeue(ctx context.Context, in *msg.Queue, opts ...grpc.CallOption) (Queue_DequeueClient, error)
}

type queueClient struct {
	cc grpc.ClientConnInterface
}

func NewQueueClient(cc grpc.ClientConnInterface) QueueClient {
	return &queueClient{cc}
}

func (c *queueClient) Create(ctx context.Context, in *msg.Queue, opts ...grpc.CallOption) (*msg.Ack, error) {
	out := new(msg.Ack)
	err := c.cc.Invoke(ctx, "/q.Queue/Create", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queueClient) Enqueue(ctx context.Context, opts ...grpc.CallOption) (Queue_EnqueueClient, error) {
	stream, err := c.cc.NewStream(ctx, &Queue_ServiceDesc.Streams[0], "/q.Queue/Enqueue", opts...)
	if err != nil {
		return nil, err
	}
	x := &queueEnqueueClient{stream}
	return x, nil
}

type Queue_EnqueueClient interface {
	Send(*msg.Message) error
	CloseAndRecv() (*WriteResult, error)
	grpc.ClientStream
}

type queueEnqueueClient struct {
	grpc.ClientStream
}

func (x *queueEnqueueClient) Send(m *msg.Message) error {
	return x.ClientStream.SendMsg(m)
}

func (x *queueEnqueueClient) CloseAndRecv() (*WriteResult, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(WriteResult)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *queueClient) Dequeue(ctx context.Context, in *msg.Queue, opts ...grpc.CallOption) (Queue_DequeueClient, error) {
	stream, err := c.cc.NewStream(ctx, &Queue_ServiceDesc.Streams[1], "/q.Queue/Dequeue", opts...)
	if err != nil {
		return nil, err
	}
	x := &queueDequeueClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Queue_DequeueClient interface {
	Recv() (*msg.Message, error)
	grpc.ClientStream
}

type queueDequeueClient struct {
	grpc.ClientStream
}

func (x *queueDequeueClient) Recv() (*msg.Message, error) {
	m := new(msg.Message)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// QueueServer is the server API for Queue service.
// All implementations must embed UnimplementedQueueServer
// for forward compatibility
type QueueServer interface {
	Create(context.Context, *msg.Queue) (*msg.Ack, error)
	Enqueue(Queue_EnqueueServer) error
	Dequeue(*msg.Queue, Queue_DequeueServer) error
	mustEmbedUnimplementedQueueServer()
}

// UnimplementedQueueServer must be embedded to have forward compatible implementations.
type UnimplementedQueueServer struct {
}

func (UnimplementedQueueServer) Create(context.Context, *msg.Queue) (*msg.Ack, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Create not implemented")
}
func (UnimplementedQueueServer) Enqueue(Queue_EnqueueServer) error {
	return status.Errorf(codes.Unimplemented, "method Enqueue not implemented")
}
func (UnimplementedQueueServer) Dequeue(*msg.Queue, Queue_DequeueServer) error {
	return status.Errorf(codes.Unimplemented, "method Dequeue not implemented")
}
func (UnimplementedQueueServer) mustEmbedUnimplementedQueueServer() {}

// UnsafeQueueServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to QueueServer will
// result in compilation errors.
type UnsafeQueueServer interface {
	mustEmbedUnimplementedQueueServer()
}

func RegisterQueueServer(s grpc.ServiceRegistrar, srv QueueServer) {
	s.RegisterService(&Queue_ServiceDesc, srv)
}

func _Queue_Create_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(msg.Queue)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueueServer).Create(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/q.Queue/Create",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueueServer).Create(ctx, req.(*msg.Queue))
	}
	return interceptor(ctx, in, info, handler)
}

func _Queue_Enqueue_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(QueueServer).Enqueue(&queueEnqueueServer{stream})
}

type Queue_EnqueueServer interface {
	SendAndClose(*WriteResult) error
	Recv() (*msg.Message, error)
	grpc.ServerStream
}

type queueEnqueueServer struct {
	grpc.ServerStream
}

func (x *queueEnqueueServer) SendAndClose(m *WriteResult) error {
	return x.ServerStream.SendMsg(m)
}

func (x *queueEnqueueServer) Recv() (*msg.Message, error) {
	m := new(msg.Message)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _Queue_Dequeue_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(msg.Queue)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(QueueServer).Dequeue(m, &queueDequeueServer{stream})
}

type Queue_DequeueServer interface {
	Send(*msg.Message) error
	grpc.ServerStream
}

type queueDequeueServer struct {
	grpc.ServerStream
}

func (x *queueDequeueServer) Send(m *msg.Message) error {
	return x.ServerStream.SendMsg(m)
}

// Queue_ServiceDesc is the grpc.ServiceDesc for Queue service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Queue_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "q.Queue",
	HandlerType: (*QueueServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Create",
			Handler:    _Queue_Create_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Enqueue",
			Handler:       _Queue_Enqueue_Handler,
			ClientStreams: true,
		},
		{
			StreamName:    "Dequeue",
			Handler:       _Queue_Dequeue_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "q/q.proto",
}

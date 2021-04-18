// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.26.0
// 	protoc        v3.15.7
// source: q/q.proto

package q

import (
	msg "github.com/sasidakh/q/msg"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type CreateResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Ack *msg.Ack `protobuf:"bytes,1,opt,name=ack,proto3" json:"ack,omitempty"`
}

func (x *CreateResponse) Reset() {
	*x = CreateResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_q_q_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *CreateResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CreateResponse) ProtoMessage() {}

func (x *CreateResponse) ProtoReflect() protoreflect.Message {
	mi := &file_q_q_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CreateResponse.ProtoReflect.Descriptor instead.
func (*CreateResponse) Descriptor() ([]byte, []int) {
	return file_q_q_proto_rawDescGZIP(), []int{0}
}

func (x *CreateResponse) GetAck() *msg.Ack {
	if x != nil {
		return x.Ack
	}
	return nil
}

type WriteResult struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Ack   *msg.Ack `protobuf:"bytes,1,opt,name=ack,proto3" json:"ack,omitempty"`
	Count uint32   `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
}

func (x *WriteResult) Reset() {
	*x = WriteResult{}
	if protoimpl.UnsafeEnabled {
		mi := &file_q_q_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *WriteResult) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*WriteResult) ProtoMessage() {}

func (x *WriteResult) ProtoReflect() protoreflect.Message {
	mi := &file_q_q_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use WriteResult.ProtoReflect.Descriptor instead.
func (*WriteResult) Descriptor() ([]byte, []int) {
	return file_q_q_proto_rawDescGZIP(), []int{1}
}

func (x *WriteResult) GetAck() *msg.Ack {
	if x != nil {
		return x.Ack
	}
	return nil
}

func (x *WriteResult) GetCount() uint32 {
	if x != nil {
		return x.Count
	}
	return 0
}

var File_q_q_proto protoreflect.FileDescriptor

var file_q_q_proto_rawDesc = []byte{
	0x0a, 0x09, 0x71, 0x2f, 0x71, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x01, 0x71, 0x1a, 0x0f,
	0x71, 0x2f, 0x6d, 0x73, 0x67, 0x2f, 0x6d, 0x73, 0x67, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22,
	0x2c, 0x0a, 0x0e, 0x43, 0x72, 0x65, 0x61, 0x74, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73,
	0x65, 0x12, 0x1a, 0x0a, 0x03, 0x61, 0x63, 0x6b, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x08,
	0x2e, 0x6d, 0x73, 0x67, 0x2e, 0x41, 0x63, 0x6b, 0x52, 0x03, 0x61, 0x63, 0x6b, 0x22, 0x3f, 0x0a,
	0x0b, 0x57, 0x72, 0x69, 0x74, 0x65, 0x52, 0x65, 0x73, 0x75, 0x6c, 0x74, 0x12, 0x1a, 0x0a, 0x03,
	0x61, 0x63, 0x6b, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x08, 0x2e, 0x6d, 0x73, 0x67, 0x2e,
	0x41, 0x63, 0x6b, 0x52, 0x03, 0x61, 0x63, 0x6b, 0x12, 0x14, 0x0a, 0x05, 0x63, 0x6f, 0x75, 0x6e,
	0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x05, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x32, 0x79,
	0x0a, 0x05, 0x51, 0x75, 0x65, 0x75, 0x65, 0x12, 0x1e, 0x0a, 0x06, 0x43, 0x72, 0x65, 0x61, 0x74,
	0x65, 0x12, 0x0a, 0x2e, 0x6d, 0x73, 0x67, 0x2e, 0x51, 0x75, 0x65, 0x75, 0x65, 0x1a, 0x08, 0x2e,
	0x6d, 0x73, 0x67, 0x2e, 0x41, 0x63, 0x6b, 0x12, 0x29, 0x0a, 0x07, 0x45, 0x6e, 0x71, 0x75, 0x65,
	0x75, 0x65, 0x12, 0x0c, 0x2e, 0x6d, 0x73, 0x67, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65,
	0x1a, 0x0e, 0x2e, 0x71, 0x2e, 0x57, 0x72, 0x69, 0x74, 0x65, 0x52, 0x65, 0x73, 0x75, 0x6c, 0x74,
	0x28, 0x01, 0x12, 0x25, 0x0a, 0x07, 0x44, 0x65, 0x71, 0x75, 0x65, 0x75, 0x65, 0x12, 0x0a, 0x2e,
	0x6d, 0x73, 0x67, 0x2e, 0x51, 0x75, 0x65, 0x75, 0x65, 0x1a, 0x0c, 0x2e, 0x6d, 0x73, 0x67, 0x2e,
	0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x30, 0x01, 0x42, 0x17, 0x5a, 0x15, 0x67, 0x69, 0x74,
	0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x73, 0x61, 0x73, 0x69, 0x64, 0x61, 0x6b, 0x68,
	0x2f, 0x71, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_q_q_proto_rawDescOnce sync.Once
	file_q_q_proto_rawDescData = file_q_q_proto_rawDesc
)

func file_q_q_proto_rawDescGZIP() []byte {
	file_q_q_proto_rawDescOnce.Do(func() {
		file_q_q_proto_rawDescData = protoimpl.X.CompressGZIP(file_q_q_proto_rawDescData)
	})
	return file_q_q_proto_rawDescData
}

var file_q_q_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_q_q_proto_goTypes = []interface{}{
	(*CreateResponse)(nil), // 0: q.CreateResponse
	(*WriteResult)(nil),    // 1: q.WriteResult
	(*msg.Ack)(nil),        // 2: msg.Ack
	(*msg.Queue)(nil),      // 3: msg.Queue
	(*msg.Message)(nil),    // 4: msg.Message
}
var file_q_q_proto_depIdxs = []int32{
	2, // 0: q.CreateResponse.ack:type_name -> msg.Ack
	2, // 1: q.WriteResult.ack:type_name -> msg.Ack
	3, // 2: q.Queue.Create:input_type -> msg.Queue
	4, // 3: q.Queue.Enqueue:input_type -> msg.Message
	3, // 4: q.Queue.Dequeue:input_type -> msg.Queue
	2, // 5: q.Queue.Create:output_type -> msg.Ack
	1, // 6: q.Queue.Enqueue:output_type -> q.WriteResult
	4, // 7: q.Queue.Dequeue:output_type -> msg.Message
	5, // [5:8] is the sub-list for method output_type
	2, // [2:5] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_q_q_proto_init() }
func file_q_q_proto_init() {
	if File_q_q_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_q_q_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*CreateResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_q_q_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*WriteResult); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_q_q_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_q_q_proto_goTypes,
		DependencyIndexes: file_q_q_proto_depIdxs,
		MessageInfos:      file_q_q_proto_msgTypes,
	}.Build()
	File_q_q_proto = out.File
	file_q_q_proto_rawDesc = nil
	file_q_q_proto_goTypes = nil
	file_q_q_proto_depIdxs = nil
}

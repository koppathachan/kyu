// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.26.0
// 	protoc        v3.12.4
// source: sub/sub.proto

package sub

import (
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

type Queue struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Qname string `protobuf:"bytes,1,opt,name=qname,proto3" json:"qname,omitempty"`
}

func (x *Queue) Reset() {
	*x = Queue{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sub_sub_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Queue) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Queue) ProtoMessage() {}

func (x *Queue) ProtoReflect() protoreflect.Message {
	mi := &file_sub_sub_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Queue.ProtoReflect.Descriptor instead.
func (*Queue) Descriptor() ([]byte, []int) {
	return file_sub_sub_proto_rawDescGZIP(), []int{0}
}

func (x *Queue) GetQname() string {
	if x != nil {
		return x.Qname
	}
	return ""
}

type Message struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Qname string `protobuf:"bytes,1,opt,name=qname,proto3" json:"qname,omitempty"`
	Data  []byte `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
}

func (x *Message) Reset() {
	*x = Message{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sub_sub_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Message) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Message) ProtoMessage() {}

func (x *Message) ProtoReflect() protoreflect.Message {
	mi := &file_sub_sub_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Message.ProtoReflect.Descriptor instead.
func (*Message) Descriptor() ([]byte, []int) {
	return file_sub_sub_proto_rawDescGZIP(), []int{1}
}

func (x *Message) GetQname() string {
	if x != nil {
		return x.Qname
	}
	return ""
}

func (x *Message) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

var File_sub_sub_proto protoreflect.FileDescriptor

var file_sub_sub_proto_rawDesc = []byte{
	0x0a, 0x0d, 0x73, 0x75, 0x62, 0x2f, 0x73, 0x75, 0x62, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12,
	0x03, 0x73, 0x75, 0x62, 0x22, 0x1d, 0x0a, 0x05, 0x51, 0x75, 0x65, 0x75, 0x65, 0x12, 0x14, 0x0a,
	0x05, 0x71, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x71, 0x6e,
	0x61, 0x6d, 0x65, 0x22, 0x33, 0x0a, 0x07, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x12, 0x14,
	0x0a, 0x05, 0x71, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x71,
	0x6e, 0x61, 0x6d, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x64, 0x61, 0x74, 0x61, 0x18, 0x02, 0x20, 0x01,
	0x28, 0x0c, 0x52, 0x04, 0x64, 0x61, 0x74, 0x61, 0x32, 0x35, 0x0a, 0x0a, 0x53, 0x75, 0x62, 0x73,
	0x63, 0x72, 0x69, 0x62, 0x65, 0x72, 0x12, 0x27, 0x0a, 0x09, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72,
	0x69, 0x62, 0x65, 0x12, 0x0a, 0x2e, 0x73, 0x75, 0x62, 0x2e, 0x51, 0x75, 0x65, 0x75, 0x65, 0x1a,
	0x0c, 0x2e, 0x73, 0x75, 0x62, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x30, 0x01, 0x42,
	0x19, 0x5a, 0x17, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x73, 0x61,
	0x73, 0x69, 0x64, 0x61, 0x6b, 0x68, 0x2f, 0x73, 0x75, 0x62, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x33,
}

var (
	file_sub_sub_proto_rawDescOnce sync.Once
	file_sub_sub_proto_rawDescData = file_sub_sub_proto_rawDesc
)

func file_sub_sub_proto_rawDescGZIP() []byte {
	file_sub_sub_proto_rawDescOnce.Do(func() {
		file_sub_sub_proto_rawDescData = protoimpl.X.CompressGZIP(file_sub_sub_proto_rawDescData)
	})
	return file_sub_sub_proto_rawDescData
}

var file_sub_sub_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_sub_sub_proto_goTypes = []interface{}{
	(*Queue)(nil),   // 0: sub.Queue
	(*Message)(nil), // 1: sub.Message
}
var file_sub_sub_proto_depIdxs = []int32{
	0, // 0: sub.Subscriber.Subscribe:input_type -> sub.Queue
	1, // 1: sub.Subscriber.Subscribe:output_type -> sub.Message
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_sub_sub_proto_init() }
func file_sub_sub_proto_init() {
	if File_sub_sub_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_sub_sub_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Queue); i {
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
		file_sub_sub_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Message); i {
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
			RawDescriptor: file_sub_sub_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_sub_sub_proto_goTypes,
		DependencyIndexes: file_sub_sub_proto_depIdxs,
		MessageInfos:      file_sub_sub_proto_msgTypes,
	}.Build()
	File_sub_sub_proto = out.File
	file_sub_sub_proto_rawDesc = nil
	file_sub_sub_proto_goTypes = nil
	file_sub_sub_proto_depIdxs = nil
}
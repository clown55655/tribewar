package proto

import goproto "google.golang.org/protobuf/proto"

// Message keeps the local proto.Message alias stable after regenerating
// common.pb.go with the modern protoc-gen-go runtime.
type Message = goproto.Message

// Marshal keeps the existing project call sites on tribeway/pkg/proto.
func Marshal(m interface{}) ([]byte, error) {
	return goproto.Marshal(m.(goproto.Message))
}

// Unmarshal keeps the existing project call sites on tribeway/pkg/proto.
func Unmarshal(data []byte, m interface{}) error {
	return goproto.Unmarshal(data, m.(goproto.Message))
}

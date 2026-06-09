package server

import (
	"context"
	"encoding/json"
	"testing"

	"tribeway/pkg/proto"
)

func TestSystemServiceLivez(t *testing.T) {
	service := NewSystemService(&BaseServer{nodeID: "node-1", nodeType: "test"})

	resp, err := service.Livez(context.Background(), &proto.BaseRequest{})
	if err != nil {
		t.Fatalf("livez: %v", err)
	}
	if resp.Code != 0 || resp.Msg != "alive" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	var status HealthStatus
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		t.Fatalf("unmarshal health status: %v", err)
	}
	if status.NodeID != "node-1" || status.NodeType != "test" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestSystemServiceReadyzTreatsMissingOptionalComponentsAsNotConfigured(t *testing.T) {
	service := NewSystemService(&BaseServer{nodeID: "node-1", nodeType: "test"})

	resp, err := service.Readyz(context.Background(), &proto.BaseRequest{})
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	if resp.Code != 0 || resp.Msg != "ready" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

package mcp

import (
	"encoding/json"
	"testing"
)

func TestWrapAndDecode(t *testing.T) {
	message, err := Wrap(Request{JSONRPC: "2.0", ID: 7, Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := Decode(message)
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != "tools/list" || request.ID != float64(7) {
		t.Fatalf("unexpected request: %+v", request)
	}
}

func TestDecodeRejectsNonMCPMessage(t *testing.T) {
	_, err := Decode([]byte(`{"type":"chat","payload":{}}`))
	if err == nil {
		t.Fatal("expected message type error")
	}
}

func TestResponseMessageUsesJSONRPCEnvelope(t *testing.T) {
	message, err := ResponseMessage("task-1", map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	var envelope Envelope
	if err := json.Unmarshal(message, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != MessageType {
		t.Fatalf("unexpected envelope type %q", envelope.Type)
	}
}

func TestDecodeRejectsMalformedAndWrongVersion(t *testing.T) {
	for _, data := range []string{"not-json", `{"type":"mcp","payload":{"jsonrpc":"1.0"}}`} {
		if _, err := Decode([]byte(data)); err == nil {
			t.Fatalf("expected decode error for %s", data)
		}
	}
}

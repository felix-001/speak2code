package mcp

import (
	"encoding/json"
	"testing"
)

func TestSessionHandlesInitializeAndToolList(t *testing.T) {
	session := Session{ServerName: "test"}
	for _, method := range []string{"initialize", "tools/list"} {
		data, _ := Wrap(Request{JSONRPC: "2.0", ID: 1, Method: method})
		response, err := session.Handle(data)
		if err != nil {
			t.Fatal(err)
		}
		var envelope Envelope
		if err := json.Unmarshal(response, &envelope); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSessionCallsCodeTool(t *testing.T) {
	called := false
	session := Session{Call: func(id, request string) { called = id == "lingxi-2" && request == "修复" }}
	params, _ := json.Marshal(map[string]any{"name": "self.code.request", "arguments": map[string]string{"request": "修复"}})
	data, _ := Wrap(Request{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params})
	if _, err := session.Handle(data); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("tool callback was not called")
	}
}

func TestSessionRejectsBadToolCalls(t *testing.T) {
	session := Session{}
	for _, name := range []string{"unknown", "self.code.request"} {
		params, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]string{}})
		data, _ := Wrap(Request{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params})
		if _, err := session.Handle(data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionRejectsInvalidParamsAndUnknownMethod(t *testing.T) {
	session := Session{}
	data, _ := Wrap(Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: []byte("bad")})
	if _, err := session.Handle(data); err == nil {
		t.Fatal("expected invalid params error")
	}
	data, _ = Wrap(Request{JSONRPC: "2.0", ID: 1, Method: "unknown"})
	if _, err := session.Handle(data); err != nil {
		t.Fatal(err)
	}
}

func TestSessionDoesNotRespondToNotifications(t *testing.T) {
	called := false
	session := Session{Call: func(_, _ string) { called = true }}
	params, _ := json.Marshal(map[string]any{"name": "self.code.request", "arguments": map[string]string{"request": "修复"}})
	data, _ := Wrap(Request{JSONRPC: "2.0", Method: "tools/call", Params: params})
	response, err := session.Handle(data)
	if err != nil || len(response) != 0 {
		t.Fatalf("notification produced response %q: %v", response, err)
	}
	if !called {
		t.Fatal("notification tool call was not executed")
	}
	data, _ = Wrap(Request{JSONRPC: "2.0", Method: "unknown"})
	if response, err = session.Handle(data); err != nil || len(response) != 0 {
		t.Fatalf("notification produced error response %q: %v", response, err)
	}
}

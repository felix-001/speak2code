package mcp

import (
	"encoding/json"
	"fmt"
)

const (
	ProtocolVersion = "2024-11-05"
	MessageType     = "mcp"
)

type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Type        int            `json:"type"`
	InputSchema map[string]any `json:"inputSchema"`
}

func Wrap(payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: MessageType, Payload: encoded})
}

func Decode(data []byte) (Request, error) {
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Request{}, fmt.Errorf("decode envelope: %w", err)
	}
	if envelope.Type != MessageType {
		return Request{}, fmt.Errorf("unexpected message type %q", envelope.Type)
	}
	var request Request
	if err := json.Unmarshal(envelope.Payload, &request); err != nil {
		return Request{}, fmt.Errorf("decode JSON-RPC payload: %w", err)
	}
	if request.JSONRPC != "2.0" {
		return Request{}, fmt.Errorf("unsupported JSON-RPC version %q", request.JSONRPC)
	}
	return request, nil
}

func ResponseMessage(id any, result any) ([]byte, error) {
	return Wrap(Response{JSONRPC: "2.0", ID: id, Result: result})
}

func ErrorMessage(id any, code int, message string) ([]byte, error) {
	return Wrap(Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}})
}

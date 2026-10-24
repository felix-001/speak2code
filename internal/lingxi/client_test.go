package lingxi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"speak2code/internal/mcp"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConnectSendsAuthAndHello(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Device-Id") != "device" || r.Header.Get("Client-Id") != "client" || r.Header.Get("Protocol-Version") != "1" {
			t.Error("missing Lingxi authentication headers")
		}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		if _, _, err := connection.ReadMessage(); err != nil {
			t.Error(err)
			return
		}
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "websocket"})
		data, _ := mcp.Wrap(mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
		_ = connection.WriteMessage(websocket.TextMessage, data)
		_, _, _ = connection.ReadMessage()
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := (Client{Config: Config{URL: url, Token: "token", DeviceID: "device", ClientID: "client"}, Session: mcp.Session{ServerName: "test"}}).Connect(ctx)
	if err == nil {
		t.Fatal("expected test server to close connection")
	}
}

func TestConnectSendsAudioFrames(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		if _, _, err := connection.ReadMessage(); err != nil {
			t.Error(err)
			return
		}
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "websocket"})
		messageType, data, err := connection.ReadMessage()
		if err != nil || messageType != websocket.BinaryMessage || string(data) != "opus-frame" {
			t.Errorf("unexpected audio frame: type=%d data=%q err=%v", messageType, data, err)
		}
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	audio := make(chan []byte, 1)
	audio <- []byte("opus-frame")
	close(audio)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := (Client{Config: Config{URL: url}, Session: mcp.Session{ServerName: "test"}, Audio: audio}).Connect(ctx)
	if err == nil {
		t.Fatal("expected test server to close connection")
	}
}

func TestConnectForwardsAudioOutput(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_, _, _ = connection.ReadMessage()
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "websocket"})
		_ = connection.WriteMessage(websocket.BinaryMessage, []byte("speech"))
	}))
	defer server.Close()
	output := make(chan []byte, 1)
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := (Client{Config: Config{URL: url}, Session: mcp.Session{ServerName: "test"}, AudioOutput: func(data []byte) { output <- data }}).Connect(ctx)
	if err == nil {
		t.Fatal("expected test server to close connection")
	}
	select {
	case data := <-output:
		if string(data) != "speech" {
			t.Fatalf("unexpected audio output %q", data)
		}
	case <-time.After(time.Second):
		t.Fatal("audio output was not forwarded")
	}
}

func TestConnectDialError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (Client{Config: Config{URL: "ws://127.0.0.1:1"}}).Connect(ctx); err == nil {
		t.Fatal("expected dial error")
	}
}

func TestConnectIgnoresBinaryAndNonMCPMessages(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _, _ = connection.ReadMessage()
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "websocket"})
		_ = connection.WriteMessage(websocket.BinaryMessage, []byte("ignored"))
		_ = connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"other"}`))
		_ = connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"mcp","payload":{"jsonrpc":"2.0","id":1,"method":"unknown"}}`))
		_, _, _ = connection.ReadMessage()
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (Client{Config: Config{URL: url}, Session: mcp.Session{ServerName: "test"}}).Connect(ctx); err == nil {
		t.Fatal("expected server close error")
	}
}

func TestConnectReportsWriteFailureAfterServerDisconnect(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		if _, _, err := connection.ReadMessage(); err != nil {
			t.Error(err)
			return
		}
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "websocket"})
		data, _ := mcp.Wrap(mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
		_ = connection.WriteMessage(websocket.TextMessage, data)
		_ = connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (Client{Config: Config{URL: url}, Session: mcp.Session{ServerName: "test"}}).Connect(ctx); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestConnectReportsResponseWriteFailure(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		if _, _, err := connection.ReadMessage(); err != nil {
			t.Error(err)
			_ = connection.Close()
			return
		}
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "websocket"})
		data, _ := mcp.Wrap(mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
		_ = connection.WriteMessage(websocket.TextMessage, data)
		_ = connection.Close()
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (Client{Config: Config{URL: url}, Session: mcp.Session{ServerName: "test"}}).Connect(ctx); err == nil {
		t.Fatal("expected response write error")
	}
}

func TestConnectRejectsInvalidServerHello(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_, _, _ = connection.ReadMessage()
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "http"})
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (Client{Config: Config{URL: url, MaxReconnects: 0}}).Connect(ctx); err == nil {
		t.Fatal("expected invalid hello error")
	}
}

func TestConnectRetriesAfterInvalidHello(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_, _, _ = connection.ReadMessage()
		if attempts == 1 {
			_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "http"})
			return
		}
		_ = connection.WriteJSON(map[string]string{"type": "hello", "transport": "websocket"})
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := (Client{Config: Config{URL: url, ReconnectDelay: time.Millisecond, MaxReconnects: 1}}).Connect(ctx)
	if err == nil || attempts != 2 {
		t.Fatalf("expected reconnect failure after second close, err=%v attempts=%d", err, attempts)
	}
}

func TestClientDefaultTiming(t *testing.T) {
	client := Client{}
	if client.handshakeTimeout() != 10*time.Second || client.reconnectDelay() != 2*time.Second {
		t.Fatal("unexpected default timing")
	}
	client.Config.HandshakeTimeout = time.Second
	client.Config.ReconnectDelay = time.Millisecond
	if client.handshakeTimeout() != time.Second || client.reconnectDelay() != time.Millisecond {
		t.Fatal("unexpected configured timing")
	}
}

func TestConnectHandshakeTimeout(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_, _, _ = connection.ReadMessage()
		select {}
	}))
	defer server.Close()
	url := "ws" + server.URL[len("http"):]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (Client{Config: Config{URL: url, HandshakeTimeout: time.Millisecond}}).Connect(ctx); err == nil {
		t.Fatal("expected handshake timeout")
	}
}

func TestConnectDialFailureHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (Client{Config: Config{URL: "ws://127.0.0.1:1", MaxReconnects: 2}}).Connect(ctx)
	if err == nil {
		t.Fatal("expected canceled connection error")
	}
}

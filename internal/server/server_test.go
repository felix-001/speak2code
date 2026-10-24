package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"speak2code/internal/task"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHealthAndStaticHandlers(t *testing.T) {
	uiPath := t.TempDir()
	server := New(task.NewService(task.MockRunner{Diff: ""}, nil, t.TempDir()), uiPath)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("unexpected health response: %d %q", response.Code, response.Body.String())
	}
	staticRequest := httptest.NewRequest(http.MethodGet, "/missing", nil)
	staticResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(staticResponse, staticRequest)
	if staticResponse.Code != http.StatusNotFound {
		t.Fatalf("unexpected static response: %d", staticResponse.Code)
	}
}

func TestUIWebSocketStartsTaskAndBroadcastsEvents(t *testing.T) {
	application := New(task.NewService(task.MockRunner{Diff: "diff"}, nil, t.TempDir()), t.TempDir())
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/ui"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteJSON(map[string]string{"type": "task", "taskId": "ui-task", "request": "change"}); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(time.Second))
	seenReady := false
	for i := 0; i < 4; i++ {
		_, data, readErr := connection.ReadMessage()
		if readErr != nil {
			t.Fatal(readErr)
		}
		var event task.Event
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		if event.TaskID != "ui-task" {
			t.Fatalf("unexpected task id: %+v", event)
		}
		if event.Type == "ready" {
			seenReady = true
			break
		}
	}
	if !seenReady {
		t.Fatal("task did not reach ready state")
	}
}

func TestUIWebSocketUsesDefaultTaskID(t *testing.T) {
	application := New(task.NewService(task.MockRunner{Diff: ""}, nil, t.TempDir()), t.TempDir())
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/ui"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteJSON(map[string]string{"type": "task", "request": "change"}); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var event task.Event
	if err := json.Unmarshal(data, &event); err != nil || event.TaskID != "task-1" {
		t.Fatalf("unexpected default task event: %+v %v", event, err)
	}
}

func TestUIWebSocketForwardsBinaryAudio(t *testing.T) {
	audio := make(chan []byte, 1)
	application := New(task.NewService(task.MockRunner{Diff: ""}, nil, t.TempDir()), t.TempDir())
	application.SetAudioSender(func(data []byte) error {
		audio <- data
		return nil
	})
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/ui"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	frame := []byte{0x01, 0x02, 0x03}
	if err := connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-audio:
		if string(received) != string(frame) {
			t.Fatalf("unexpected audio frame %v", received)
		}
	case <-time.After(time.Second):
		t.Fatal("audio frame was not forwarded")
	}
}

func TestBroadcastAudioSendsBinaryFrameToUI(t *testing.T) {
	application := New(task.NewService(task.MockRunner{Diff: ""}, nil, t.TempDir()), t.TempDir())
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/ui"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	frame := []byte("speech")
	application.BroadcastAudio(frame)
	connection.SetReadDeadline(time.Now().Add(time.Second))
	messageType, data, err := connection.ReadMessage()
	if err != nil || messageType != websocket.BinaryMessage || string(data) != string(frame) {
		t.Fatalf("unexpected audio broadcast: type=%d data=%q err=%v", messageType, data, err)
	}
}

func TestBroadcastRemovesClosedClient(t *testing.T) {
	application := New(task.NewService(task.MockRunner{Diff: ""}, nil, t.TempDir()), t.TempDir())
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/ui"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	time.Sleep(10 * time.Millisecond)
	application.broadcast(task.Event{Type: "status", Text: "test"})
	application.mu.Lock()
	defer application.mu.Unlock()
	if len(application.clients) != 0 {
		t.Fatalf("closed client was not removed: %d", len(application.clients))
	}
}

func TestLingxiWebSocketHandlesMCPRequestAndBinaryMessage(t *testing.T) {
	application := New(task.NewService(task.MockRunner{Diff: ""}, nil, t.TempDir()), t.TempDir())
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/lingxi"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteMessage(websocket.BinaryMessage, []byte("ignored")); err != nil {
		t.Fatal(err)
	}
	request := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	wrapped, err := json.Marshal(map[string]any{"type": "mcp", "payload": json.RawMessage(request)})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteMessage(websocket.TextMessage, wrapped); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty MCP response")
	}
}

func TestStaticPathUsesDefault(t *testing.T) {
	if got := StaticPath(); got != "frontend" {
		t.Fatalf("unexpected default path %q", got)
	}
}

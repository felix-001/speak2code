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

func TestUIWebSocketSendsTextToLingxi(t *testing.T) {
	application := New(task.NewService(task.MockRunner{Diff: "diff"}, nil, t.TempDir()), t.TempDir())
	application.SetTextSender(func(text string) error {
		if text != "change" {
			t.Errorf("unexpected request: %q", text)
		}
		application.BroadcastSpeech("灵矽回复")
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
	var initial task.Event
	if err := connection.ReadJSON(&initial); err != nil || initial.Type != "lingxi_connection" || initial.Text != "true" {
		t.Fatalf("unexpected connection: %+v %v", initial, err)
	}
	if err := connection.WriteJSON(map[string]string{"type": "task", "taskId": "ui-task", "request": "change"}); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(time.Second))
	for i := 0; i < 1; i++ {
		_, data, readErr := connection.ReadMessage()
		if readErr != nil {
			t.Fatal(readErr)
		}
		var event task.Event
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "lingxi_speech" || event.Text != "灵矽回复" {
			t.Fatalf("unexpected response: %+v", event)
		}
	}
}

func TestUIVoiceOrdering(t *testing.T) {
	application := New(task.NewService(task.MockRunner{}, nil, t.TempDir()), t.TempDir())
	if application.sendVoice("start", nil) == nil {
		t.Fatal("disconnected voice accepted")
	}
	received := make(chan string, 4)
	application.SetVoiceSender(func(state string, audio []byte) error {
		if state == "" {
			state = string(audio)
		}
		received <- state
		return nil
	})
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+httpServer.URL[4:]+"/ws/ui", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _, _ = connection.ReadMessage()
	_ = connection.WriteJSON(map[string]string{"type": "listen", "state": "start"})
	_ = connection.WriteMessage(websocket.BinaryMessage, []byte("opus"))
	_ = connection.WriteJSON(map[string]string{"type": "listen", "state": "stop"})
	for _, expected := range []string{"start", "opus", "stop"} {
		select {
		case actual := <-received:
			if actual != expected {
				t.Fatalf("got %q want %q", actual, expected)
			}
		case <-time.After(time.Second):
			t.Fatal("voice forwarding timed out")
		}
	}
	_ = connection.WriteJSON(map[string]string{"type": "listen", "state": "start"})
	<-received
	_ = connection.Close()
	select {
	case actual := <-received:
		if actual != "stop" {
			t.Fatal(actual)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect did not stop listening")
	}
}

func TestUIWebSocketReportsDisconnectedLingxi(t *testing.T) {
	application := New(task.NewService(task.MockRunner{Diff: ""}, nil, t.TempDir()), t.TempDir())
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/ui"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var initial task.Event
	if err := connection.ReadJSON(&initial); err != nil || initial.Text != "false" {
		t.Fatalf("unexpected connection: %+v %v", initial, err)
	}
	if err := connection.WriteJSON(map[string]string{"type": "task", "request": "change"}); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var event task.Event
	if err := json.Unmarshal(data, &event); err != nil || event.Type != "lingxi_error" || event.Error == "" {
		t.Fatalf("unexpected default task event: %+v %v", event, err)
	}
}

func TestSendTextValidation(t *testing.T) {
	application := New(task.NewService(task.MockRunner{}, nil, t.TempDir()), t.TempDir())
	if application.sendText(" ") == nil {
		t.Fatal("expected empty request error")
	}
	application.SetTextSender(func(string) error { return fmt.Errorf("send failed") })
	if application.sendText("hello") == nil {
		t.Fatal("expected send error")
	}
	application.SetTextSender(nil)
	if application.sendText("hello") == nil {
		t.Fatal("expected disconnected error")
	}
}

func TestMCPTaskEntry(t *testing.T) {
	application := New(task.NewService(task.MockRunner{}, nil, t.TempDir()), t.TempDir())
	application.LingxiSession().Call("", "")
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
	application.BroadcastAudioRate(24000)
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	url := "ws" + httpServer.URL[len("http"):] + "/ws/ui"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var initial task.Event
	if err := connection.ReadJSON(&initial); err != nil || initial.Type != "lingxi_connection" {
		t.Fatalf("unexpected connection: %+v %v", initial, err)
	}
	if err := connection.ReadJSON(&initial); err != nil || initial.Type != "lingxi_audio" || initial.Text != "24000" {
		t.Fatalf("unexpected audio rate: %+v %v", initial, err)
	}
	frame := []byte("speech")
	application.BroadcastInput("用户语音转写")
	connection.SetReadDeadline(time.Now().Add(time.Second))
	var input task.Event
	if err := connection.ReadJSON(&input); err != nil || input.Type != "lingxi_input" || input.Text != "用户语音转写" || input.TaskID != "" {
		t.Fatalf("unexpected input broadcast: %+v err=%v", input, err)
	}
	application.BroadcastSpeech("灵矽语音文字")
	connection.SetReadDeadline(time.Now().Add(time.Second))
	var speech task.Event
	if err := connection.ReadJSON(&speech); err != nil || speech.Type != "lingxi_speech" || speech.Text != "灵矽语音文字" || speech.TaskID != "" {
		t.Fatalf("unexpected speech broadcast: %+v err=%v", speech, err)
	}
	application.BroadcastAudio(frame)
	connection.SetReadDeadline(time.Now().Add(time.Second))
	messageType, data, err := connection.ReadMessage()
	if err != nil || messageType != websocket.BinaryMessage || string(data) != string(frame) {
		t.Fatalf("unexpected audio broadcast: type=%d data=%q err=%v", messageType, data, err)
	}
}

func TestPlaybackHasSingleRecipient(t *testing.T) {
	application := New(task.NewService(task.MockRunner{}, nil, t.TempDir()), t.TempDir())
	httpServer := httptest.NewServer(application.Handler())
	defer httpServer.Close()
	connect := func() *websocket.Conn {
		connection, _, err := websocket.DefaultDialer.Dial("ws"+httpServer.URL[4:]+"/ws/ui", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = connection.Close() })
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err := connection.ReadMessage(); err != nil {
			t.Fatal(err)
		}
		return connection
	}
	first := connect()
	second := connect()
	var revoked task.Event
	if err := first.ReadJSON(&revoked); err != nil || revoked.Type != "playback_revoked" {
		t.Fatalf("missing revocation: %+v %v", revoked, err)
	}
	application.BroadcastAudio([]byte("second only"))
	if kind, data, err := second.ReadMessage(); err != nil || kind != websocket.BinaryMessage || string(data) != "second only" {
		t.Fatalf("unexpected audio: %d %q %v", kind, data, err)
	}
	_ = first.WriteJSON(map[string]string{"type": "playback", "state": "claim"})
	if err := second.ReadJSON(&revoked); err != nil || revoked.Type != "playback_revoked" {
		t.Fatal("claim did not revoke previous owner", err)
	}
	application.BroadcastAudio([]byte("first only"))
	if kind, data, err := first.ReadMessage(); err != nil || kind != websocket.BinaryMessage || string(data) != "first only" {
		t.Fatalf("old audio leaked or claim failed: %d %q %v", kind, data, err)
	}
	_ = first.WriteJSON(map[string]string{"type": "playback", "state": "release"})
	_ = first.WriteJSON(map[string]string{"type": "task", "request": "barrier"})
	var barrier task.Event
	if err := first.ReadJSON(&barrier); err != nil {
		t.Fatal(err)
	}
	application.BroadcastAudio([]byte("nobody"))
	application.BroadcastSpeech("barrier")
	if kind, _, err := first.ReadMessage(); err != nil || kind != websocket.TextMessage {
		t.Fatal("released owner received audio", err)
	}
	if kind, _, err := second.ReadMessage(); err != nil || kind != websocket.TextMessage {
		t.Fatal("non-owner received audio", err)
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

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"speak2code/internal/mcp"
	"speak2code/internal/task"
	"sync"

	"github.com/gorilla/websocket"
)

type Server struct {
	Service  *task.Service
	UIPath   string
	Audio    func([]byte) error
	upgrader websocket.Upgrader
	clients  map[*websocket.Conn]struct{}
	mu       sync.Mutex
}

func (s *Server) SetAudioSender(sender func([]byte) error) {
	s.Audio = sender
}

func (s *Server) BroadcastSpeech(text string) {
	s.broadcast(task.Event{Type: "lingxi_speech", Text: text})
}

func (s *Server) BroadcastAudio(data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for client := range s.clients {
		if err := client.WriteMessage(websocket.BinaryMessage, data); err != nil {
			_ = client.Close()
			delete(s.clients, client)
		}
	}
}

func New(service *task.Service, uiPath string) *Server {
	return &Server{Service: service, UIPath: uiPath, clients: map[*websocket.Conn]struct{}{}, upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ws/ui", s.ui)
	mux.HandleFunc("/ws/lingxi", s.lingxi)
	mux.Handle("/", http.FileServer(http.Dir(s.UIPath)))
	return mux
}
func (s *Server) broadcast(event task.Event) {
	s.Service.Remember(event)
	data := task.EncodeEvent(event)
	s.mu.Lock()
	defer s.mu.Unlock()
	for client := range s.clients {
		if err := client.WriteMessage(websocket.TextMessage, data); err != nil {
			_ = client.Close()
			delete(s.clients, client)
		}
	}
}
func (s *Server) ui(w http.ResponseWriter, r *http.Request) {
	client, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.clients[client] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.clients, client); s.mu.Unlock(); _ = client.Close() }()
	for {
		messageType, data, err := client.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.BinaryMessage {
			if s.Audio != nil && len(data) > 0 {
				_ = s.Audio(data)
			}
			continue
		}
		var request struct {
			Type    string `json:"type"`
			TaskID  string `json:"taskId"`
			Request string `json:"request"`
		}
		if json.Unmarshal(data, &request) == nil && request.Type == "task" {
			s.startTask(request.TaskID, request.Request)
		}
	}
}
func (s *Server) startTask(taskID, request string) {
	if taskID == "" {
		taskID = "task-1"
	}
	s.Service.Start(context.Background(), taskID, request, s.broadcast)
}
func (s *Server) LingxiSession() mcp.Session {
	return mcp.Session{ServerName: "speak2code-mcp", Call: s.startTask}
}
func (s *Server) lingxi(w http.ResponseWriter, r *http.Request) {
	client, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	session := s.LingxiSession()
	for {
		messageType, data, err := client.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		response, err := session.Handle(data)
		if err != nil {
			response, _ = mcp.ErrorMessage(nil, -32600, err.Error())
		}
		_ = client.WriteMessage(websocket.TextMessage, response)
	}
}
func StaticPath() string {
	return "frontend"
}

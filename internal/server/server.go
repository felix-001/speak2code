package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"speak2code/internal/mcp"
	"speak2code/internal/task"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// clientWriteTimeout prevents a stalled client from blocking broadcasts.
const clientWriteTimeout = 10 * time.Second

type Server struct {
	Service        *task.Service
	UIPath         string
	Audio          func([]byte) error
	textSender     func(string) error
	voiceSender    func(string, []byte) error
	audioRate      int
	upgrader       websocket.Upgrader
	clients        map[*websocket.Conn]struct{}
	playbackClient *websocket.Conn
	mu             sync.Mutex
}

func (s *Server) SetTextSender(sender func(string) error) {
	s.mu.Lock()
	s.textSender = sender
	s.mu.Unlock()
	s.broadcast(task.Event{Type: "lingxi_connection", Text: strconv.FormatBool(sender != nil)})
}

func (s *Server) SetVoiceSender(sender func(string, []byte) error) {
	s.mu.Lock()
	s.voiceSender = sender
	s.mu.Unlock()
}

func (s *Server) sendVoice(state string, audio []byte) error {
	s.mu.Lock()
	sender := s.voiceSender
	s.mu.Unlock()
	if sender == nil {
		return fmt.Errorf("灵矽语音未连接")
	}
	return sender(state, audio)
}

func (s *Server) BroadcastAudioRate(rate int) {
	s.mu.Lock()
	s.audioRate = rate
	s.mu.Unlock()
	s.broadcast(task.Event{Type: "lingxi_audio", Text: strconv.Itoa(rate)})
}

func (s *Server) sendText(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("需求不能为空")
	}
	s.mu.Lock()
	sender := s.textSender
	s.mu.Unlock()
	if sender == nil {
		return fmt.Errorf("灵矽未连接，请检查服务配置或稍后重试")
	}
	return sender(text)
}

func (s *Server) SetAudioSender(sender func([]byte) error) {
	s.mu.Lock()
	s.Audio = sender
	s.mu.Unlock()
}

func (s *Server) BroadcastSpeech(text string) {
	s.broadcast(task.Event{Type: "lingxi_speech", Text: text})
}

func (s *Server) BroadcastInput(text string) {
	s.broadcast(task.Event{Type: "lingxi_input", Text: text})
}

func (s *Server) BroadcastAudio(data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if client := s.playbackClient; client != nil {
		if err := writeClient(client, websocket.BinaryMessage, data); err != nil {
			_ = client.Close()
			delete(s.clients, client)
			s.playbackClient = nil
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
		if err := writeClient(client, websocket.TextMessage, data); err != nil {
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
	s.claimPlayback(client)
	_ = client.WriteMessage(websocket.TextMessage, task.EncodeEvent(task.Event{Type: "lingxi_connection", Text: strconv.FormatBool(s.textSender != nil)}))
	if s.audioRate > 0 {
		_ = client.WriteMessage(websocket.TextMessage, task.EncodeEvent(task.Event{Type: "lingxi_audio", Text: strconv.Itoa(s.audioRate)}))
	}
	s.mu.Unlock()
	voiceActive := false
	defer func() {
		if voiceActive {
			_ = s.sendVoice("stop", nil)
		}
	}()
	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		if s.playbackClient == client {
			s.playbackClient = nil
		}
		s.mu.Unlock()
		_ = client.Close()
	}()
	for {
		messageType, data, err := client.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.BinaryMessage {
			if voiceActive {
				if err := s.sendVoice("", data); err != nil {
					s.broadcast(task.Event{Type: "lingxi_error", Error: err.Error()})
				}
				continue
			}
			s.mu.Lock()
			audio := s.Audio
			s.mu.Unlock()
			if audio != nil && len(data) > 0 {
				_ = audio(data)
			}
			continue
		}
		var request struct {
			Type    string `json:"type"`
			State   string `json:"state"`
			TaskID  string `json:"taskId"`
			Request string `json:"request"`
		}
		if json.Unmarshal(data, &request) != nil {
			continue
		}
		if request.Type == "playback" {
			s.mu.Lock()
			if request.State == "claim" {
				s.claimPlayback(client)
			} else if request.State == "release" && s.playbackClient == client {
				s.playbackClient = nil
			}
			s.mu.Unlock()
		}
		if request.Type == "listen" && (request.State == "start" || request.State == "stop") {
			if err := s.sendVoice(request.State, nil); err != nil {
				s.broadcast(task.Event{Type: "lingxi_error", Error: err.Error()})
			} else {
				voiceActive = request.State == "start"
			}
		}
		if request.Type == "task" {
			if err := s.sendText(request.Request); err != nil {
				s.broadcast(task.Event{Type: "lingxi_error", Error: err.Error()})
			}
		}
	}
}
func (s *Server) claimPlayback(client *websocket.Conn) {
	if previous := s.playbackClient; previous != nil && previous != client {
		_ = writeClient(previous, websocket.TextMessage, task.EncodeEvent(task.Event{Type: "playback_revoked"}))
	}
	s.playbackClient = client
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
			request, _ := mcp.Decode(data)
			response, _ = mcp.ErrorMessage(request.ID, -32600, err.Error())
		}
		if len(response) == 0 {
			continue
		}
		_ = client.WriteMessage(websocket.TextMessage, response)
	}
}
func writeClient(client *websocket.Conn, messageType int, data []byte) error {
	if err := client.SetWriteDeadline(time.Now().Add(clientWriteTimeout)); err != nil {
		return err
	}
	return client.WriteMessage(messageType, data)
}
func StaticPath() string {
	return "frontend"
}

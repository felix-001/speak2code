package lingxi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"speak2code/internal/mcp"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	URL, Token, DeviceID, ClientID   string
	HandshakeTimeout, ReconnectDelay time.Duration
	MaxReconnects                    int
}

type Client struct {
	Config      Config
	Session     mcp.Session
	Audio       <-chan []byte
	AudioOutput func([]byte)
	SpeechText  func(string)
	InputText   func(string)
	SenderReady func(func(string) error)
	VoiceReady  func(func(string, []byte) error)
	AudioRate   func(int)
}

func (c Client) Connect(ctx context.Context) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		log.Printf("lingxi connect: attempt=%d", attempt+1)
		lastErr = c.connectOnce(ctx)
		log.Printf("lingxi disconnected: attempt=%d error=%v", attempt+1, lastErr)
		if lastErr == nil || (c.Config.MaxReconnects >= 0 && attempt >= c.Config.MaxReconnects) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.reconnectDelay()):
		}
	}
}

func (c Client) connectOnce(ctx context.Context) error {
	header := http.Header{
		"Authorization":    []string{"Bearer " + c.Config.Token},
		"Device-Id":        []string{c.Config.DeviceID},
		"Client-Id":        []string{c.Config.ClientID},
		"Protocol-Version": []string{"1"},
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, c.Config.URL, header)
	if err != nil {
		return fmt.Errorf("lingxi websocket: %w", err)
	}
	defer connection.Close()
	log.Printf("lingxi websocket connected")
	initialized, toolsListed := false, false
	defer func() {
		log.Printf("lingxi MCP session ended: initialized=%t tools_listed=%t", initialized, toolsListed)
	}()
	var writeMu sync.Mutex
	writeMessage := func(messageType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return connection.WriteMessage(messageType, data)
	}
	hello := map[string]any{
		"type": "hello", "version": 1,
		"features": map[string]any{"mcp": true}, "transport": "websocket",
		"audio_params": map[string]any{"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60, "play_buffer_duration": 1000},
	}
	helloData, err := json.Marshal(hello)
	if err != nil {
		return fmt.Errorf("lingxi hello: %w", err)
	}
	if err := writeMessage(websocket.TextMessage, helloData); err != nil {
		return err
	}
	log.Printf("lingxi hello sent: mcp=true protocol=1 audio=opus")
	if err := connection.SetReadDeadline(time.Now().Add(c.handshakeTimeout())); err != nil {
		return err
	}
	messageType, data, err := connection.ReadMessage()
	if err != nil {
		return fmt.Errorf("lingxi hello: %w", err)
	}
	if messageType != websocket.TextMessage || !validHello(data) {
		return fmt.Errorf("lingxi hello: invalid server response")
	}
	var helloResponse struct {
		SessionID   string `json:"session_id"`
		AudioParams struct {
			SampleRate int `json:"sample_rate"`
		} `json:"audio_params"`
	}
	_ = json.Unmarshal(data, &helloResponse)
	log.Printf("lingxi hello accepted: session_present=%t audio_rate=%d", helloResponse.SessionID != "", helloResponse.AudioParams.SampleRate)
	if c.VoiceReady != nil {
		c.VoiceReady(func(state string, audio []byte) error {
			if state == "" {
				return writeMessage(websocket.BinaryMessage, audio)
			}
			if state != "start" && state != "stop" {
				return fmt.Errorf("invalid listen state: %s", state)
			}
			message, err := json.Marshal(map[string]string{"type": "listen", "state": state, "mode": "manual", "session_id": helloResponse.SessionID})
			if err != nil {
				return err
			}
			return writeMessage(websocket.TextMessage, message)
		})
		defer c.VoiceReady(nil)
		log.Printf("lingxi voice sender ready")
	}
	if c.AudioRate != nil {
		rate := helloResponse.AudioParams.SampleRate
		if rate <= 0 {
			rate = 16000
		}
		c.AudioRate(rate)
	}
	if c.SenderReady != nil {
		c.SenderReady(func(text string) error {
			message, err := json.Marshal(map[string]string{"type": "listen", "state": "detect", "text": text, "session_id": helloResponse.SessionID})
			if err != nil {
				return err
			}
			return writeMessage(websocket.TextMessage, message)
		})
		defer c.SenderReady(nil)
	}
	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	stopAudio := make(chan struct{})
	var audioWG sync.WaitGroup
	defer func() {
		close(stopAudio)
		audioWG.Wait()
	}()
	if c.Audio != nil {
		audioWG.Add(1)
		go func() {
			defer audioWG.Done()
			for {
				select {
				case <-stopAudio:
					return
				case audio, ok := <-c.Audio:
					if !ok {
						return
					}
					if len(audio) > 0 {
						_ = writeMessage(websocket.BinaryMessage, audio)
					}
				}
			}
		}()
	}
	for {
		messageType, data, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		if messageType != websocket.TextMessage {
			if messageType == websocket.BinaryMessage && c.AudioOutput != nil && len(data) > 0 {
				c.AudioOutput(append([]byte(nil), data...))
			}
			continue
		}
		var speech struct {
			Type  string `json:"type"`
			State string `json:"state"`
			Text  string `json:"text"`
		}
		if json.Unmarshal(data, &speech) == nil && speech.Type == "tts" && speech.State == "sentence_start" && speech.Text != "" && c.SpeechText != nil {
			c.SpeechText(speech.Text)
		}
		if speech.Type == "stt" && speech.Text != "" && c.InputText != nil {
			c.InputText(speech.Text)
		}
		var envelope mcp.Envelope
		if json.Unmarshal(data, &envelope) != nil || envelope.Type != mcp.MessageType {
			continue
		}
		request, decodeErr := mcp.Decode(data)
		log.Printf("lingxi MCP received: method=%q has_id=%t decode_error=%v", request.Method, request.ID != nil, decodeErr)
		response, err := c.Session.Handle(data)
		if err != nil {
			response, _ = mcp.ErrorMessage(nil, -32600, err.Error())
		}
		if err := writeMessage(websocket.TextMessage, response); err != nil {
			log.Printf("lingxi MCP response failed: method=%q error=%v", request.Method, err)
			return err
		}
		var responseEnvelope mcp.Envelope
		var rpcResponse mcp.Response
		validResponse := json.Unmarshal(response, &responseEnvelope) == nil && json.Unmarshal(responseEnvelope.Payload, &rpcResponse) == nil
		success := validResponse && rpcResponse.Error == nil
		log.Printf("lingxi MCP response sent: method=%q success=%t", request.Method, success)
		if success && request.Method == "initialize" {
			initialized = true
			log.Printf("lingxi MCP initialized: protocol=%s", mcp.ProtocolVersion)
		}
		if success && request.Method == "tools/list" {
			toolsListed = true
			log.Printf("lingxi MCP tools advertised: tools=self.code.request initialized=%t", initialized)
		}
	}
}

func (c Client) handshakeTimeout() time.Duration {
	if c.Config.HandshakeTimeout > 0 {
		return c.Config.HandshakeTimeout
	}
	return 10 * time.Second
}

func (c Client) reconnectDelay() time.Duration {
	if c.Config.ReconnectDelay > 0 {
		return c.Config.ReconnectDelay
	}
	return 2 * time.Second
}

func validHello(data []byte) bool {
	var hello struct {
		Type      string `json:"type"`
		Transport string `json:"transport"`
	}
	return json.Unmarshal(data, &hello) == nil && hello.Type == "hello" && hello.Transport == "websocket"
}

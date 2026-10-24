package lingxi

import (
	"context"
	"encoding/json"
	"fmt"
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
}

func (c Client) Connect(ctx context.Context) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		lastErr = c.connectOnce(ctx)
		if lastErr == nil || attempt >= c.Config.MaxReconnects {
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
		var envelope mcp.Envelope
		if json.Unmarshal(data, &envelope) != nil || envelope.Type != mcp.MessageType {
			continue
		}
		response, err := c.Session.Handle(data)
		if err != nil {
			response, _ = mcp.ErrorMessage(nil, -32600, err.Error())
		}
		if err := writeMessage(websocket.TextMessage, response); err != nil {
			return err
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

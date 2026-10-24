package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server Server `yaml:"server"`
	Lingxi Lingxi `yaml:"lingxi"`
	Codex  Codex  `yaml:"codex"`
	GitHub GitHub `yaml:"github"`
}

type Server struct {
	Address   string `yaml:"address"`
	UIPath    string `yaml:"ui_path"`
	Workspace string `yaml:"workspace"`
}

type Lingxi struct {
	URL                  string        `yaml:"url"`
	Token                string        `yaml:"token"`
	DeviceID             string        `yaml:"device_id"`
	ClientID             string        `yaml:"client_id"`
	HandshakeTimeout     time.Duration `yaml:"-"`
	ReconnectDelay       time.Duration `yaml:"-"`
	HandshakeTimeoutText string        `yaml:"handshake_timeout"`
	ReconnectDelayText   string        `yaml:"reconnect_delay"`
	MaxReconnects        int           `yaml:"max_reconnects"`
}

type Codex struct {
	Command string `yaml:"command"`
}

type GitHub struct {
	Token string `yaml:"token"`
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var value Config
	if err := yaml.Unmarshal(data, &value); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if value.Server.Address == "" {
		value.Server.Address = ":8080"
	}
	if value.Server.UIPath == "" {
		value.Server.UIPath = "frontend"
	}
	if value.Server.Workspace == "" {
		value.Server.Workspace = "."
	}
	if value.Codex.Command == "" {
		value.Codex.Command = "codex"
	}
	value.Lingxi.HandshakeTimeout = parseDuration(value.Lingxi.HandshakeTimeoutText, 10*time.Second)
	value.Lingxi.ReconnectDelay = parseDuration(value.Lingxi.ReconnectDelayText, 2*time.Second)
	return value, nil
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}

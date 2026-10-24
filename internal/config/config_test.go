package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("lingxi:\n  url: ws://example\ncodex:\n  command: codex-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := Load(path)
	if err != nil || value.Server.Address != ":8080" || value.Server.UIPath != "frontend" || value.Codex.Command != "codex-test" {
		t.Fatalf("unexpected config: %+v %v", value, err)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected read error")
	}
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("server: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadDurationFallbacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("lingxi:\n  handshake_timeout: invalid\n  reconnect_delay: -1s\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := Load(path)
	if err != nil || value.Lingxi.HandshakeTimeout.String() != "10s" || value.Lingxi.ReconnectDelay.String() != "2s" {
		t.Fatalf("unexpected duration defaults: %+v %v", value.Lingxi, err)
	}
}

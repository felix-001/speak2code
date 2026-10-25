package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"speak2code/internal/config"
	"speak2code/internal/github"
	"speak2code/internal/lingxi"
	"speak2code/internal/server"
	"speak2code/internal/task"
	"strings"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}
	if err := run(configPath); err != nil {
		log.Fatal(err)
	}
}

func run(configPath string) error {
	logFile, err := os.OpenFile("/tmp/speak2code.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("server startup: config=%s cwd=%s", configPath, mustWorkingDirectory())
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("config load failed: error=%v", err)
		return err
	}
	workspace, err := filepath.Abs(cfg.Server.Workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	if err := validateWorkspace(workspace); err != nil {
		log.Printf("workspace validation failed: workspace=%s error=%v", workspace, err)
		return err
	}
	cfg.Server.UIPath = resolveUIPath(configPath, cfg.Server.UIPath)
	application := newApplication(cfg)
	log.Printf("server configured: workspace=%s ui_path=%s lingxi_url=%s", cfg.Server.Workspace, cfg.Server.UIPath, cfg.Lingxi.URL)
	address := cfg.Server.Address
	log.Printf("speak2code MCP server listening on http://localhost%s", address)
	return serve(address, application)
}

func validateWorkspace(workspace string) error {
	info, err := os.Stat(workspace)
	if err != nil {
		return fmt.Errorf("workspace %s: %w", workspace, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace %s is not a directory", workspace)
	}
	command := exec.Command("git", "-C", workspace, "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("workspace %s is not a Git repository; set server.workspace to the project repository (for example /Users/rigensen/workspace/aicoding) and initialize it with git init: %w", workspace, err)
	}
	log.Printf("workspace git root: configured=%s root=%s", workspace, strings.TrimSpace(string(output)))
	return nil
}

func mustWorkingDirectory() string {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "<unknown>"
	}
	return workingDirectory
}

func resolveUIPath(configPath, uiPath string) string {
	if filepath.IsAbs(uiPath) {
		return uiPath
	}
	paths := []string{filepath.Join(filepath.Dir(configPath), uiPath), uiPath}
	if workingDirectory, err := os.Getwd(); err == nil {
		for directory := workingDirectory; ; directory = filepath.Dir(directory) {
			paths = append(paths, filepath.Join(directory, uiPath))
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
		}
	}
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
			absolute, err := filepath.Abs(path)
			if err == nil {
				return absolute
			}
		}
	}
	return uiPath
}

func newApplication(cfg config.Config) *server.Server {
	workspace, err := filepath.Abs(cfg.Server.Workspace)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("workspace resolved: configured=%s absolute=%s", cfg.Server.Workspace, workspace)
	runner := task.CommandRunner{Workspace: workspace, Command: cfg.Codex.Command}
	var prs task.PRClient
	if cfg.GitHub.Token != "" && cfg.GitHub.Owner != "" && cfg.GitHub.Repo != "" {
		prs = github.Client{Token: cfg.GitHub.Token, Owner: cfg.GitHub.Owner, Repo: cfg.GitHub.Repo}
	}
	service := task.NewService(runner, prs, workspace)
	service.Push = prs != nil
	application := server.New(service, cfg.Server.UIPath)
	audio := make(chan []byte, 16)
	application.SetAudioSender(func(data []byte) error {
		select {
		case audio <- append([]byte(nil), data...):
			return nil
		default:
			return fmt.Errorf("lingxi audio buffer is full")
		}
	})
	if cfg.Lingxi.URL != "" {
		cfg.Lingxi.MaxReconnects = -1
		go connectLingxiWithAudio(cfg, application, audio)
	}
	return application
}

func connectLingxi(cfg config.Config, application *server.Server) {
	connectLingxiWithAudio(cfg, application, nil)
}

func connectLingxiWithAudio(cfg config.Config, application *server.Server, audio <-chan []byte) {
	client := lingxi.Client{Config: lingxi.Config{URL: cfg.Lingxi.URL, Token: cfg.Lingxi.Token, DeviceID: cfg.Lingxi.DeviceID, ClientID: cfg.Lingxi.ClientID, HandshakeTimeout: cfg.Lingxi.HandshakeTimeout, ReconnectDelay: cfg.Lingxi.ReconnectDelay, MaxReconnects: cfg.Lingxi.MaxReconnects}, Session: application.LingxiSession(), AudioOutput: application.BroadcastAudio}
	client.Audio = audio
	client.InputText = application.BroadcastInput
	client.SpeechText = application.BroadcastSpeech
	client.SenderReady = application.SetTextSender
	client.VoiceReady = application.SetVoiceSender
	client.AudioRate = application.BroadcastAudioRate
	if err := client.Connect(context.Background()); err != nil {
		log.Printf("灵矽连接失败: %v", err)
	}
}

func serve(address string, application *server.Server) error {
	return http.ListenAndServe(address, application.Handler())
}

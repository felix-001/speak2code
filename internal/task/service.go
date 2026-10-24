package task

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Event struct {
	TaskID string `json:"taskId"`
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Diff   string `json:"diff,omitempty"`
	PR     *PR    `json:"pr,omitempty"`
	Error  string `json:"error,omitempty"`
}

type Runner interface {
	Run(context.Context, string, func(Event)) (string, error)
}
type PRClient interface {
	Create(context.Context, string, string) (PR, error)
}
type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
}

type Service struct {
	runner    Runner
	prs       PRClient
	workspace string
	Push      bool
	mu        sync.Mutex
	events    []Event
}

func NewService(runner Runner, prs PRClient, workspace string) *Service {
	return &Service{runner: runner, prs: prs, workspace: workspace}
}
func (s *Service) Start(ctx context.Context, taskID, request string, publish func(Event)) {
	if strings.TrimSpace(request) == "" {
		log.Printf("task ignored: task_id=%s reason=empty_request", taskID)
		return
	}
	go func() {
		log.Printf("task start: task_id=%s workspace=%s request=%q", taskID, s.workspace, request)
		publish(Event{TaskID: taskID, Type: "status", Text: "已接收需求"})
		diff, err := s.runner.Run(ctx, request, func(event Event) { event.TaskID = taskID; publish(event) })
		if err != nil {
			log.Printf("task failed: task_id=%s error=%v", taskID, err)
			publish(Event{TaskID: taskID, Type: "error", Error: err.Error()})
			return
		}
		log.Printf("task codex completed: task_id=%s diff_bytes=%d", taskID, len(diff))
		publish(Event{TaskID: taskID, Type: "diff", Diff: diff})
		if s.prs == nil {
			publish(Event{TaskID: taskID, Type: "ready", Text: "编码完成，等待确认"})
			return
		}
		branch := "lingxi/" + taskID
		publish(Event{TaskID: taskID, Type: "git", Text: "正在创建分支并提交工作区变更：" + branch})
		if err := commitWorkspace(ctx, s.workspace, branch); err != nil {
			publish(Event{TaskID: taskID, Type: "error", Error: err.Error()})
			return
		}
		if s.Push {
			publish(Event{TaskID: taskID, Type: "git", Text: "提交完成，正在推送分支：" + branch})
			if err := pushWorkspace(ctx, s.workspace, branch); err != nil {
				publish(Event{TaskID: taskID, Type: "error", Error: err.Error()})
				return
			}
		}
		publish(Event{TaskID: taskID, Type: "git", Text: "正在创建 GitHub Pull Request"})
		pr, err := s.prs.Create(ctx, branch, diff)
		if err != nil {
			publish(Event{TaskID: taskID, Type: "error", Error: err.Error()})
			return
		}
		publish(Event{TaskID: taskID, Type: "pr", PR: &pr, Text: "已创建 Pull Request，等待合并"})
	}()
}
func (s *Service) Remember(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

type CommandRunner struct {
	Workspace string
	Command   string
}

func (r CommandRunner) Run(ctx context.Context, request string, publish func(Event)) (string, error) {
	command := r.Command
	if command == "" {
		command = "codex"
	}
	publish(Event{Type: "thinking", Text: "正在请求 Codex Agent 分析任务"})
	cmd := exec.CommandContext(ctx, command, "exec", "--json", "--sandbox", "workspace-write", "--skip-git-repo-check", request)
	cmd.Dir = r.Workspace
	cmd.Stdin = nil
	log.Printf("codex start: command=%s args=%q workspace=%s", command, cmd.Args[1:], r.Workspace)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("codex stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		log.Printf("codex start failed: workspace=%s error=%v", r.Workspace, err)
		return "", fmt.Errorf("codex: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if text := codexEventText(scanner.Bytes()); text != "" {
			publish(Event{Type: "thinking", Text: text})
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("codex stream: %w", err)
	}
	err = cmd.Wait()
	if err != nil {
		log.Printf("codex exit failed: workspace=%s error=%v stderr=%q", r.Workspace, err, strings.TrimSpace(stderr.String()))
		return "", fmt.Errorf("codex: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	log.Printf("codex exit: workspace=%s", r.Workspace)
	publish(Event{Type: "thinking", Text: "Codex 已完成编码与验证"})
	publish(Event{Type: "git", Text: "正在标记新文件并读取 git diff"})
	diff, err := gitDiff(ctx, r.Workspace)
	if err != nil {
		return "", err
	}
	return diff, nil
}

func codexEventText(data []byte) string {
	var event struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message string `json:"message"`
		Item    struct {
			Text    string `json:"text"`
			Command string `json:"command"`
			Output  string `json:"aggregated_output"`
			Status  string `json:"status"`
		} `json:"item"`
	}
	if json.Unmarshal(data, &event) != nil {
		return strings.TrimSpace(string(data))
	}
	if event.Item.Command != "" {
		return strings.TrimSpace(event.Item.Command + "\n" + event.Item.Status + "\n" + event.Item.Output)
	}
	for _, text := range []string{event.Text, event.Message, event.Item.Text} {
		if strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
func gitDiff(ctx context.Context, workspace string) (string, error) {
	intentToAdd := exec.CommandContext(ctx, "git", "add", "-N", "--", ".")
	intentToAdd.Dir = workspace
	if output, err := intentToAdd.CombinedOutput(); err != nil {
		log.Printf("git add intent-to-add failed: workspace=%s error=%v output=%q", workspace, err, strings.TrimSpace(string(output)))
		return "", fmt.Errorf("git add intent-to-add: %w: %s", err, strings.TrimSpace(string(output)))
	}
	log.Printf("git diff start: workspace=%s command=%q", workspace, []string{"git", "diff", "--", "."})
	command := exec.CommandContext(ctx, "git", "diff", "--", ".")
	command.Dir = workspace
	output, err := command.CombinedOutput()
	if err != nil {
		log.Printf("git diff failed: workspace=%s error=%v output=%q", workspace, err, strings.TrimSpace(string(output)))
		return "", fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(string(output)))
	}
	log.Printf("git diff completed: workspace=%s diff_bytes=%d", workspace, len(output))
	return string(output), nil
}

func commitWorkspace(ctx context.Context, workspace, branch string) error {
	commands := [][]string{{"checkout", "-B", branch}, {"add", "-A"}, {"commit", "-m", "灵矽编码任务 " + branch}}
	for _, args := range commands {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = workspace
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func pushWorkspace(ctx context.Context, workspace, branch string) error {
	command := exec.CommandContext(ctx, "git", "push", "-u", "origin", branch)
	command.Dir = workspace
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

type MockRunner struct {
	Diff  string
	Delay time.Duration
}

func (r MockRunner) Run(ctx context.Context, _ string, publish func(Event)) (string, error) {
	publish(Event{Type: "thinking", Text: "模拟 Codex Agent 正在分析工作区"})
	if r.Delay > 0 {
		select {
		case <-time.After(r.Delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return r.Diff, nil
}
func EncodeEvent(event Event) []byte { value, _ := json.Marshal(event); return value }

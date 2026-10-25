package task

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testRunner struct{}

func (testRunner) Run(_ context.Context, _ string, publish func(Event)) (string, error) {
	publish(Event{Type: "thinking", Text: "ok"})
	return "diff --git a/a b/a", nil
}

type testPR struct{ called bool }

func (p *testPR) Create(_ context.Context, branch, _ string) (PR, error) {
	p.called = branch == "lingxi/task"
	return PR{Number: 1, URL: "https://github.com/example/repo/pull/1"}, nil
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, func(Event)) (string, error) {
	return "", context.DeadlineExceeded
}

type countingRunner struct{ calls chan struct{} }

func (r countingRunner) Run(context.Context, string, func(Event)) (string, error) {
	r.calls <- struct{}{}
	return "", nil
}

type testRunnerWithChange struct{ workspace string }

func (r testRunnerWithChange) Run(_ context.Context, _ string, publish func(Event)) (string, error) {
	publish(Event{Type: "thinking", Text: "ok"})
	if err := os.WriteFile(filepath.Join(r.workspace, "file.txt"), []byte("after"), 0600); err != nil {
		return "", err
	}
	return "diff --git a/file.txt b/file.txt", nil
}

func TestMockRunnerCancellationAndDiff(t *testing.T) {
	runner := MockRunner{Diff: "diff"}
	events := 0
	diff, err := runner.Run(context.Background(), "request", func(Event) { events++ })
	if err != nil || diff != "diff" || events != 1 {
		t.Fatalf("unexpected result: %q %v %d", diff, err, events)
	}
}

func TestMockRunnerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (MockRunner{Delay: time.Second}).Run(ctx, "request", func(Event) {}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestServiceIgnoresEmptyRequest(t *testing.T) {
	calls := make(chan struct{}, 1)
	service := NewService(countingRunner{calls: calls}, nil, t.TempDir())
	service.Start(context.Background(), "task", " \t\n", func(Event) {})
	select {
	case <-calls:
		t.Fatal("empty request started the runner")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCommandRunnerReportsCommandError(t *testing.T) {
	_, err := (CommandRunner{Workspace: t.TempDir(), Command: "missing-codex"}).Run(context.Background(), "request", func(Event) {})
	if err == nil {
		t.Fatal("expected codex command error")
	}
	_, err = (CommandRunner{Workspace: t.TempDir()}).Run(context.Background(), "request", func(Event) {})
	if err == nil {
		t.Fatal("expected default codex command error")
	}
}

func TestCodexEventText(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"text", `{"text":"thinking"}`, "thinking"},
		{"message", `{"message":"running"}`, "running"},
		{"item", `{"item":{"text":"changed file"}}`, "changed file"},
		{"plain", " raw output ", "raw output"},
		{"empty", `{"type":"completed"}`, ""},
		{"command", `{"item":{"command":"git status","status":"completed","aggregated_output":"clean"}}`, "git status\ncompleted\nclean"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := codexEventText([]byte(test.data)); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestCommandRunnerStreamsJSONAndStderr(t *testing.T) {
	workspace := t.TempDir()
	runner := CommandRunner{Workspace: workspace, Command: "/bin/sh"}
	events := []Event{}
	_, err := runner.Run(context.Background(), `-c`, func(event Event) { events = append(events, event) })
	if err == nil {
		t.Fatal("expected shell argument error")
	}
	if len(events) == 0 {
		t.Fatal("expected initial thinking event")
	}
}
func TestEncodeEvent(t *testing.T) {
	if string(EncodeEvent(Event{TaskID: "1", Type: "status"})) == "" {
		t.Fatal("event was not encoded")
	}
}
func TestServiceRemember(t *testing.T) {
	service := NewService(testRunner{}, nil, t.TempDir())
	service.Remember(Event{Type: "status"})
	if len(service.events) != 1 {
		t.Fatal("event was not remembered")
	}
}

func TestServiceStartWithoutPR(t *testing.T) {
	service := NewService(testRunner{}, nil, t.TempDir())
	events := make(chan Event, 4)
	service.Start(context.Background(), "task", "request", func(event Event) { events <- event })
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "ready" {
				return
			}
		case <-deadline:
			t.Fatal("task did not finish")
		}
	}
}

func TestServiceStartReportsRunnerError(t *testing.T) {
	service := NewService(failingRunner{}, nil, t.TempDir())
	events := make(chan Event, 2)
	service.Start(context.Background(), "task", "request", func(event Event) { events <- event })
	if event := <-events; event.Type != "status" {
		t.Fatalf("unexpected first event: %+v", event)
	}
	if event := <-events; event.Type != "error" {
		t.Fatalf("unexpected error event: %+v", event)
	}
}

func TestCommitWorkspace(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := commitWorkspace(context.Background(), workspace, "lingxi/task"); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("git", "-C", workspace, "branch", "--show-current").Output()
	if err != nil || string(output) != "lingxi/task\n" {
		t.Fatalf("unexpected branch %q: %v", output, err)
	}
}

func TestPushWorkspaceReportsMissingRemote(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	if err := pushWorkspace(context.Background(), workspace, "lingxi/task"); err == nil {
		t.Fatal("expected missing remote error")
	}
}

func TestPushWorkspace(t *testing.T) {
	workspace := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, remoteDir(t, remote), "init", "--bare")
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "initial")
	runGit(t, workspace, "remote", "add", "origin", remote)
	runGit(t, workspace, "checkout", "-B", "lingxi/task")
	if err := pushWorkspace(context.Background(), workspace, "lingxi/task"); err != nil {
		t.Fatal(err)
	}
}

func remoteDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGitDiffReportsInvalidWorkspace(t *testing.T) {
	if _, err := gitDiff(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected git diff error")
	}
}

func runGit(t *testing.T, workspace string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", workspace}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestCommandRunnerUsesCodexAndReadsDiff(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "init")
	command := filepath.Join(workspace, "fake-codex")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '{\"text\":\"editing file\"}\\n'\nprintf changed > file.txt\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{Workspace: workspace, Command: command}
	events := []Event{}
	diff, err := runner.Run(context.Background(), "request", func(event Event) { events = append(events, event) })
	if err != nil || diff == "" || !filepath.IsAbs(command) {
		t.Fatalf("unexpected codex result: %q %v", diff, err)
	}
	if len(events) < 3 || events[1].Text != "editing file" {
		t.Fatalf("unexpected streamed events: %+v", events)
	}
}

func TestCommandRunnerReadsNewFilesFromCodex(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	command := filepath.Join(workspace, "fake-codex")
	if err := os.WriteFile(command, []byte("#!/bin/sh\n[ \"$3\" = '--sandbox' ] && [ \"$4\" = 'workspace-write' ] || exit 1\nprintf '{\"text\":\"creating file\"}\\n'\nprintf 'package main\\n\\nfunc main() {}\\n' > hello.go\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{Workspace: workspace, Command: command}
	diff, err := runner.Run(context.Background(), "request", func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "hello.go") {
		t.Fatalf("expected new file in diff, got %q", diff)
	}
}

func TestGitDiffKeepsIgnoredFilesUntracked(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	for name, content := range map[string]string{".gitignore": "secret.txt\n", "secret.txt": "secret", "hello.go": "package main\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	diff, err := gitDiff(context.Background(), workspace)
	if err != nil || !strings.Contains(diff, "hello.go") || strings.Contains(diff, "secret.txt b/secret.txt") {
		t.Fatalf("unexpected diff: %q, error: %v", diff, err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git", "index.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := gitDiff(context.Background(), workspace); err == nil {
		t.Fatal("expected locked index error")
	}
}

func TestServiceCreatesPRAfterCommit(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "--allow-empty", "-m", "init")
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "initial")
	prs := &testPR{}
	service := NewService(testRunnerWithChange{workspace: workspace}, prs, workspace)
	events := make(chan Event, 8)
	service.Start(context.Background(), "task", "request", func(event Event) { events <- event })
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "pr" {
				if !prs.called {
					t.Fatal("PR client was not called")
				}
				return
			}
		case <-deadline:
			t.Fatal("PR was not created")
		}
	}
}

func TestServicePushesBeforeCreatingPR(t *testing.T) {
	workspace := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, remoteDir(t, remote), "init", "--bare")
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	runGit(t, workspace, "remote", "add", "origin", remote)
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "--allow-empty", "-m", "initial")
	service := NewService(testRunnerWithChange{workspace: workspace}, &testPR{}, workspace)
	service.Push = true
	events := make(chan Event, 8)
	service.Start(context.Background(), "task", "request", func(event Event) { events <- event })
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "pr" {
				return
			}
		case <-deadline:
			t.Fatal("PR was not created after push")
		}
	}
}

func TestServiceReportsCommitAndPRErrors(t *testing.T) {
	workspace := t.TempDir()
	commitEvents := make(chan Event, 8)
	NewService(testRunner{}, &testPR{}, workspace).Start(context.Background(), "task", "request", func(event Event) { commitEvents <- event })
	waitForTaskError(t, commitEvents)
	workspace = t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "initial")
	prEvents := make(chan Event, 8)
	NewService(testRunner{}, failingPR{}, workspace).Start(context.Background(), "task", "request", func(event Event) { prEvents <- event })
	waitForTaskError(t, prEvents)
}

func TestServiceReportsPushError(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "--allow-empty", "-m", "initial")
	service := NewService(testRunner{}, &testPR{}, workspace)
	service.Push = true
	events := make(chan Event, 8)
	service.Start(context.Background(), "task", "request", func(event Event) { events <- event })
	waitForTaskError(t, events)
}

func waitForTaskError(t *testing.T, events <-chan Event) {
	t.Helper()
	deadline := time.After(time.Second)
	seenGit := false
	for {
		select {
		case event := <-events:
			seenGit = seenGit || event.Type == "git"
			if event.Type == "error" {
				if !seenGit {
					t.Fatal("missing Git progress before error")
				}
				return
			}
		case <-deadline:
			t.Fatal("expected task error")
		}
	}
}

type failingPR struct{}

func (failingPR) Create(context.Context, string, string) (PR, error) {
	return PR{}, context.DeadlineExceeded
}

func TestServiceRunsTasksSequentially(t *testing.T) {
	runner := &blockingRunner{entered: make(chan int, 2), release: make(chan struct{})}
	service := NewService(runner, nil, t.TempDir())
	service.Start(context.Background(), "first", "request", func(Event) {})
	if id := <-runner.entered; id != 1 {
		t.Fatalf("unexpected first task %d", id)
	}
	service.Start(context.Background(), "second", "request", func(Event) {})
	select {
	case id := <-runner.entered:
		t.Fatalf("task %d ran concurrently", id)
	case <-time.After(100 * time.Millisecond):
	}
	close(runner.release)
	if id := <-runner.entered; id != 2 {
		t.Fatalf("unexpected second task %d", id)
	}
}

type blockingRunner struct {
	entered chan int
	release chan struct{}
	mu      sync.Mutex
	count   int
}

func (r *blockingRunner) Run(ctx context.Context, _ string, _ func(Event)) (string, error) {
	r.mu.Lock()
	r.count++
	id := r.count
	r.mu.Unlock()
	r.entered <- id
	select {
	case <-r.release:
		return "", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRememberCapsEventHistory(t *testing.T) {
	service := NewService(testRunner{}, nil, t.TempDir())
	for i := 0; i < maxRememberedEvents+10; i++ {
		service.Remember(Event{Type: "status"})
	}
	if len(service.events) != maxRememberedEvents {
		t.Fatalf("expected capped history, got %d", len(service.events))
	}
}

func TestCommandRunnerReadsLongEventLines(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "--allow-empty", "-m", "initial")
	command := filepath.Join(workspace, "fake-codex")
	longText := strings.Repeat("x", 200*1024)
	script := fmt.Sprintf("#!/bin/sh\nprintf '{\"text\":\"%s\"}\\n'\n", longText)
	if err := os.WriteFile(command, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{Workspace: workspace, Command: command}
	var longest int
	if _, err := runner.Run(context.Background(), "request", func(event Event) {
		if len(event.Text) > longest {
			longest = len(event.Text)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if longest < len(longText) {
		t.Fatalf("long event line was truncated: %d", longest)
	}
}

func TestGitDiffLeavesIndexClean(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	if err := os.WriteFile(filepath.Join(workspace, "hello.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	diff, err := gitDiff(context.Background(), workspace)
	if err != nil || !strings.Contains(diff, "hello.go") {
		t.Fatalf("unexpected diff: %q, error: %v", diff, err)
	}
	command := exec.Command("git", "-C", workspace, "ls-files", "--", "hello.go")
	output, err := command.Output()
	if err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("intent-to-add entry leaked into index: %q, %v", output, err)
	}
}

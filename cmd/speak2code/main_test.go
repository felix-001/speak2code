package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"speak2code/internal/config"
	"testing"
)

func TestNewApplicationWithoutGitHub(t *testing.T) {
	application := newApplication(config.Config{Server: config.Server{Workspace: t.TempDir(), UIPath: t.TempDir()}})
	if application == nil || application.Service == nil || application.Service.Push {
		t.Fatal("expected local application without GitHub push")
	}
}

func TestNewApplicationWithGitHub(t *testing.T) {
	application := newApplication(config.Config{Server: config.Server{Workspace: t.TempDir(), UIPath: t.TempDir()}, GitHub: config.GitHub{Token: "token", Owner: "owner", Repo: "repo"}})
	if application == nil || application.Service == nil || !application.Service.Push {
		t.Fatal("expected GitHub-enabled application")
	}
}

func TestNewApplicationAudioBufferBackpressure(t *testing.T) {
	application := newApplication(config.Config{Server: config.Server{Workspace: t.TempDir(), UIPath: t.TempDir()}})
	for index := 0; index < 16; index++ {
		if err := application.Audio([]byte("audio")); err != nil {
			t.Fatalf("unexpected audio buffer error at %d: %v", index, err)
		}
	}
	if err := application.Audio([]byte("audio")); err == nil {
		t.Fatal("expected audio buffer backpressure error")
	}
}

func TestConnectLingxiReportsConnectionFailure(t *testing.T) {
	connectLingxi(config.Config{Lingxi: config.Lingxi{URL: "ws://127.0.0.1:1"}}, newApplication(config.Config{Server: config.Server{Workspace: t.TempDir(), UIPath: t.TempDir()}}))
}

func TestServeReportsInvalidAddress(t *testing.T) {
	if err := serve("invalid address", newApplication(config.Config{Server: config.Server{Workspace: t.TempDir(), UIPath: t.TempDir()}})); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestRunReportsMissingConfig(t *testing.T) {
	if err := run(t.TempDir() + "/missing.yaml"); err == nil {
		t.Fatal("expected missing config error")
	}
}

func TestValidateWorkspaceRequiresGitRepository(t *testing.T) {
	workspace := t.TempDir()
	if err := validateWorkspace(workspace); err == nil {
		t.Fatal("expected non-Git workspace error")
	}
	runGit(t, workspace, "init")
	if err := validateWorkspace(workspace); err != nil {
		t.Fatalf("expected Git workspace to validate: %v", err)
	}
	if err := validateWorkspace(filepath.Join(workspace, "missing")); err == nil {
		t.Fatal("expected missing workspace error")
	}
}

func TestRunRejectsInvalidWorkspace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  workspace: "+filepath.Join(root, "missing")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspace(path); err == nil {
		t.Fatal("expected file workspace error")
	}
	if err := run(path); err == nil {
		t.Fatal("expected invalid workspace startup error")
	}
}

func runGit(t *testing.T, workspace string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", workspace}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestRunLoadsConfigBeforeServe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("server:\n  address: invalid address\n  ui_path: .\n  workspace: .\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(path); err == nil {
		t.Fatal("expected invalid listen address")
	}
}

func TestResolveUIPathFindsFrontendFromConfigDirectory(t *testing.T) {
	root := t.TempDir()
	uiPath := filepath.Join(root, "frontend")
	if err := os.MkdirAll(uiPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uiPath, "index.html"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.yaml")
	got := resolveUIPath(configPath, "frontend")
	want, err := filepath.Abs(uiPath)
	if err != nil || got != want {
		t.Fatalf("got UI path %q, want %q, err=%v", got, want, err)
	}
}

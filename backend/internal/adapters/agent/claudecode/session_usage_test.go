package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestSessionUsage_StateFileNotFound(t *testing.T) {
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: "/nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for missing state file")
	}
	if usage != nil {
		t.Fatal("expected nil usage")
	}
}

func TestSessionUsage_ReadsStateFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	worktreeName := "my-project"
	stateDir := filepath.Join(home, ".claude", "projects", worktreeName)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(stateDir, "state.json")
	stateJSON := `{"model":"claude-sonnet-4-20250514","inputTokens":1000,"outputTokens":400,"cost":0.009,"contextUtilization":0.5,"retryCount":2}`
	if err := os.WriteFile(stateFile, []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: "/home/user/" + worktreeName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", usage.SessionID)
	}
	if usage.InputTokens != 1000 {
		t.Fatalf("InputTokens = %d, want 1000", usage.InputTokens)
	}
	if usage.OutputTokens != 400 {
		t.Fatalf("OutputTokens = %d, want 400", usage.OutputTokens)
	}
	if usage.Cost != 0.009 {
		t.Fatalf("Cost = %f, want 0.009", usage.Cost)
	}
	if usage.ContextPct != 0.5 {
		t.Fatalf("ContextPct = %f, want 0.5", usage.ContextPct)
	}
	if usage.RetryCount != 2 {
		t.Fatalf("RetryCount = %d, want 2", usage.RetryCount)
	}
}

func TestSessionUsage_EmptyStateFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	worktreeName := "empty-project"
	stateDir := filepath.Join(home, ".claude", "projects", worktreeName)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(stateDir, "state.json")
	if err := os.WriteFile(stateFile, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: "/home/user/" + worktreeName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for empty state")
	}
	if usage != nil {
		t.Fatal("expected nil usage")
	}
}

func TestSessionUsage_BadJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	worktreeName := "bad-project"
	stateDir := filepath.Join(home, ".claude", "projects", worktreeName)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(stateDir, "state.json")
	if err := os.WriteFile(stateFile, []byte(`not json`), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: "/home/user/" + worktreeName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for bad JSON")
	}
	if usage != nil {
		t.Fatal("expected nil usage")
	}
}

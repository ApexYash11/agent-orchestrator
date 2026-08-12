package opencode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestSessionUsage_NoAgentSessionID(t *testing.T) {
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", Metadata: map[string]string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when no agent session ID")
	}
	if usage != nil {
		t.Fatal("expected nil usage")
	}
}

func TestSessionUsage_StateFileNotFound(t *testing.T) {
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: "/nonexistent",
		Metadata: map[string]string{opencodeAgentSessionIDMetadataKey: "ses_missing"},
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
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".opencode", "sessions", "ses_abc123")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(stateDir, "state.json")
	stateJSON := `{"model":"claude-sonnet-4-20250514","inputTokens":500,"outputTokens":200,"cost":0.005,"contextUtilization":0.35,"retryCount":1}`
	if err := os.WriteFile(stateFile, []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: dir,
		Metadata: map[string]string{opencodeAgentSessionIDMetadataKey: "ses_abc123"},
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
	if usage.InputTokens != 500 {
		t.Fatalf("InputTokens = %d, want 500", usage.InputTokens)
	}
	if usage.OutputTokens != 200 {
		t.Fatalf("OutputTokens = %d, want 200", usage.OutputTokens)
	}
	if usage.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("Model = %q, want claude-sonnet-4-20250514", usage.Model)
	}
	if usage.Cost != 0.005 {
		t.Fatalf("Cost = %f, want 0.005", usage.Cost)
	}
	if usage.ContextPct != 0.35 {
		t.Fatalf("ContextPct = %f, want 0.35", usage.ContextPct)
	}
	if usage.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", usage.RetryCount)
	}
}

func TestSessionUsage_EmptyStateFile(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".opencode", "sessions", "ses_empty")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(stateDir, "state.json")
	if err := os.WriteFile(stateFile, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: dir,
		Metadata: map[string]string{opencodeAgentSessionIDMetadataKey: "ses_empty"},
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
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".opencode", "sessions", "ses_bad")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(stateDir, "state.json")
	if err := os.WriteFile(stateFile, []byte(`not json`), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Plugin{}
	usage, ok, err := p.SessionUsage(context.Background(), ports.SessionRef{
		ID: "sess-1", WorkspacePath: dir,
		Metadata: map[string]string{opencodeAgentSessionIDMetadataKey: "ses_bad"},
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

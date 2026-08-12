package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.UsageProvider = (*Plugin)(nil)

// claudeSessionStatePath returns the expected path for a Claude Code session's
// state file. The exact filename and JSON shape should be verified against a
// real Claude Code session during initial implementation.
func claudeSessionStatePath(workspacePath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	worktreeName := filepath.Base(workspacePath)
	return filepath.Join(home, ".claude", "projects", worktreeName, "state.json")
}

// claudeSessionState is the JSON shape we expect from the Claude Code state file.
type claudeSessionState struct {
	Model        string  `json:"model,omitempty"`
	InputTokens  int64   `json:"inputTokens,omitempty"`
	OutputTokens int64   `json:"outputTokens,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
	ContextPct   float64 `json:"contextUtilization,omitempty"`
	RetryCount   int64   `json:"retryCount,omitempty"`
}

func (p *Plugin) SessionUsage(ctx context.Context, session ports.SessionRef) (*domain.AgentUsage, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	statePath := claudeSessionStatePath(session.WorkspacePath)
	if statePath == "" {
		return nil, false, nil
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("claude-code: read state: %w", err)
	}

	var state claudeSessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, nil
	}

	if state.InputTokens == 0 && state.OutputTokens == 0 && state.Cost == 0 && state.Model == "" {
		return nil, false, nil
	}

	usage := domain.NewAgentUsage(session.ID)
	usage.InputTokens = state.InputTokens
	usage.OutputTokens = state.OutputTokens
	usage.Model = state.Model
	usage.ContextPct = state.ContextPct
	usage.RetryCount = state.RetryCount
	if state.Cost > 0 {
		usage.Cost = state.Cost
	}
	usage.RecordedAt = time.Now().UTC()
	return &usage, true, nil
}

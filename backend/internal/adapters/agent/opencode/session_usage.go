package opencode

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

// opencodeSessionStatePath returns the expected path for an opencode session's
// state file within the workspace. The exact filename and JSON shape should be
// verified against a real opencode session during initial implementation.
func opencodeSessionStatePath(workspacePath, agentSessionID string) string {
	return filepath.Join(workspacePath, ".opencode", "sessions", agentSessionID, "state.json")
}

// opencodeSessionState is the JSON shape we expect from the opencode state file.
type opencodeSessionState struct {
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

	agentSessionID := session.Metadata[opencodeAgentSessionIDMetadataKey]
	if agentSessionID == "" {
		return nil, false, nil
	}

	statePath := opencodeSessionStatePath(session.WorkspacePath, agentSessionID)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("opencode: read state: %w", err)
	}

	var state opencodeSessionState
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

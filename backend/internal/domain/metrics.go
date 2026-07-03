package domain

import "time"

type AgentUsage struct {
	SessionID    string
	InputTokens  int64
	OutputTokens int64
	Cost         float64
	Model        string
	ContextPct   float64
	RetryCount   int64
	RecordedAt   time.Time
}

func NewAgentUsage(sessionID string) AgentUsage {
	return AgentUsage{SessionID: sessionID, Cost: -1}
}

type SessionMetrics struct {
	SessionID          string
	TotalInputTokens   int64
	TotalOutputTokens  int64
	EstimatedCost      float64
	Model              string
	ContextUtilization float64
	RetryCount         int64
	LastActivityAt     time.Time
	UpdatedAt          time.Time
	RecordedAt         time.Time
}

func (u *AgentUsage) FillCost() {
	if u.Cost != -1 || u.Model == "" {
		return
	}
	if cost, ok := ComputeCost(u.Model, u.InputTokens, u.OutputTokens); ok {
		u.Cost = cost
	}
}

package domain

import (
	"testing"
	"time"
)

func TestNewAgentUsage(t *testing.T) {
	u := NewAgentUsage("sess-1")
	if u.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", u.SessionID)
	}
	if u.Cost != -1 {
		t.Fatalf("Cost = %f, want -1 (sentinel)", u.Cost)
	}
}

func TestFillCost_AlreadySet(t *testing.T) {
	u := AgentUsage{SessionID: "s", Cost: 0.05}
	u.FillCost()
	if u.Cost != 0.05 {
		t.Fatalf("FillCost should not overwrite a non-sentinel cost; got %f", u.Cost)
	}
}

func TestFillCost_EmptyModel(t *testing.T) {
	u := AgentUsage{SessionID: "s", Cost: -1, InputTokens: 100, OutputTokens: 50}
	u.FillCost()
	if u.Cost != -1 {
		t.Fatalf("FillCost on empty model should not compute cost; got %f", u.Cost)
	}
}

func TestFillCost_KnownModel(t *testing.T) {
	u := AgentUsage{SessionID: "s", Cost: -1, Model: "claude-sonnet-4-20250514", InputTokens: 1000, OutputTokens: 500}
	u.FillCost()
	if u.Cost == -1 || u.Cost <= 0 {
		t.Fatalf("FillCost with claude-sonnet-4 should compute a positive cost; got %g", u.Cost)
	}
	expected := 1000*3.0/1_000_000 + 500*15.0/1_000_000
	if abs(u.Cost-expected) > 1e-9 {
		t.Fatalf("Cost = %g, want %g", u.Cost, expected)
	}
}

func TestFillCost_UnknownModel(t *testing.T) {
	u := AgentUsage{SessionID: "s", Cost: -1, Model: "nonexistent-model-v1", InputTokens: 100, OutputTokens: 50}
	u.FillCost()
	if u.Cost != -1 {
		t.Fatalf("FillCost with unknown model should keep sentinel; got %f", u.Cost)
	}
}

func TestSessionMetrics_ZeroValues(t *testing.T) {
	sm := SessionMetrics{}
	if sm.SessionID != "" {
		t.Fatalf("zero SessionMetrics should have empty SessionID")
	}
	if sm.LastActivityAt != (time.Time{}) {
		t.Fatalf("zero SessionMetrics should have zero LastActivityAt")
	}
}

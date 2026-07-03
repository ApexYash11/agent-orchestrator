package metrics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeSessionLister struct {
	list func(ctx context.Context) ([]domain.SessionRecord, error)
}

func (f *fakeSessionLister) ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error) {
	return f.list(ctx)
}

type fakeMetricsStore struct {
	recordUsage       func(ctx context.Context, usage domain.AgentUsage) error
	upsertCurrent     func(ctx context.Context, sm domain.SessionMetrics) error
	getSessionMetrics func(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
}

func (f *fakeMetricsStore) RecordUsage(ctx context.Context, usage domain.AgentUsage) error {
	return f.recordUsage(ctx, usage)
}

func (f *fakeMetricsStore) UpsertSessionMetricsCurrent(ctx context.Context, sm domain.SessionMetrics) error {
	return f.upsertCurrent(ctx, sm)
}

func (f *fakeMetricsStore) GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error) {
	return f.getSessionMetrics(ctx, sessionID)
}

type fakeUsageProvider struct {
	sessionUsage func(ctx context.Context, ref ports.SessionRef) (*domain.AgentUsage, bool, error)
}

func (f *fakeUsageProvider) SessionUsage(ctx context.Context, ref ports.SessionRef) (*domain.AgentUsage, bool, error) {
	return f.sessionUsage(ctx, ref)
}

type fakeAgent struct {
	ports.Agent
	provider ports.UsageProvider
}

func (f *fakeAgent) SessionUsage(ctx context.Context, ref ports.SessionRef) (*domain.AgentUsage, bool, error) {
	if f.provider != nil {
		return f.provider.SessionUsage(ctx, ref)
	}
	return nil, false, nil
}

type fakeAgentResolver struct {
	agent func(harness domain.AgentHarness) (ports.Agent, bool)
}

func (f *fakeAgentResolver) Agent(harness domain.AgentHarness) (ports.Agent, bool) {
	return f.agent(harness)
}

func TestCollector_PollSkipsTerminated(t *testing.T) {
	store := &fakeMetricsStore{
		getSessionMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return nil, nil
		},
	}
	collector := NewCollector(
		&fakeSessionLister{
			list: func(_ context.Context) ([]domain.SessionRecord, error) {
				return []domain.SessionRecord{
					{ID: "sess-dead", IsTerminated: true, Harness: "claude-code"},
				}, nil
			},
		},
		&fakeAgentResolver{
			agent: func(_ domain.AgentHarness) (ports.Agent, bool) {
				return &fakeAgent{}, true
			},
		},
		store,
		quietLogger(),
		time.Minute,
	)
	err := collector.poll(context.Background())
	if err != nil {
		t.Fatalf("poll() = %v, want nil", err)
	}
}

func TestCollector_PollSkipsNoAgent(t *testing.T) {
	store := &fakeMetricsStore{
		getSessionMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return nil, nil
		},
	}
	collector := NewCollector(
		&fakeSessionLister{
			list: func(_ context.Context) ([]domain.SessionRecord, error) {
				return []domain.SessionRecord{
					{ID: "sess-1", IsTerminated: false, Harness: "nonexistent"},
				}, nil
			},
		},
		&fakeAgentResolver{
			agent: func(_ domain.AgentHarness) (ports.Agent, bool) {
				return nil, false
			},
		},
		store,
		quietLogger(),
		time.Minute,
	)
	err := collector.poll(context.Background())
	if err != nil {
		t.Fatalf("poll() = %v, want nil", err)
	}
}

func TestCollector_PollSkipsNoUsageProvider(t *testing.T) {
	store := &fakeMetricsStore{}
	collector := NewCollector(
		&fakeSessionLister{
			list: func(_ context.Context) ([]domain.SessionRecord, error) {
				return []domain.SessionRecord{
					{ID: "sess-1", IsTerminated: false, Harness: "claude-code"},
				}, nil
			},
		},
		&fakeAgentResolver{
			agent: func(_ domain.AgentHarness) (ports.Agent, bool) {
				return &fakeAgent{}, true
			},
		},
		store,
		quietLogger(),
		time.Minute,
	)
	err := collector.poll(context.Background())
	if err != nil {
		t.Fatalf("poll() = %v, want nil", err)
	}
}

func TestCollector_PollRecordsUsage(t *testing.T) {
	recorded := false
	upserted := false
	store := &fakeMetricsStore{
		getSessionMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return nil, nil
		},
		recordUsage: func(_ context.Context, u domain.AgentUsage) error {
			recorded = true
			if u.SessionID != "sess-1" {
				t.Fatalf("RecordUsage sessionID = %q, want sess-1", u.SessionID)
			}
			return nil
		},
		upsertCurrent: func(_ context.Context, _ domain.SessionMetrics) error {
			upserted = true
			return nil
		},
	}
	collector := NewCollector(
		&fakeSessionLister{
			list: func(_ context.Context) ([]domain.SessionRecord, error) {
				return []domain.SessionRecord{
					{
						ID: "sess-1", IsTerminated: false, Harness: "claude-code",
						UpdatedAt: time.Now().UTC(),
						Metadata:  domain.SessionMetadata{WorkspacePath: "/ws/sess-1"},
					},
				}, nil
			},
		},
		&fakeAgentResolver{
			agent: func(_ domain.AgentHarness) (ports.Agent, bool) {
				return &fakeAgent{
					provider: &fakeUsageProvider{
						sessionUsage: func(_ context.Context, ref ports.SessionRef) (*domain.AgentUsage, bool, error) {
							return &domain.AgentUsage{
								SessionID: "sess-1", InputTokens: 100, OutputTokens: 50,
								Model: "claude-sonnet-4-20250514", ContextPct: 0.45, RetryCount: 1,
							}, true, nil
						},
					},
				}, true
			},
		},
		store,
		quietLogger(),
		time.Minute,
	)
	err := collector.poll(context.Background())
	if err != nil {
		t.Fatalf("poll() = %v, want nil", err)
	}
	if !recorded {
		t.Fatal("RecordUsage was not called")
	}
	if !upserted {
		t.Fatal("UpsertSessionMetricsCurrent was not called")
	}
}

func TestCollector_PollSessionListError(t *testing.T) {
	collector := NewCollector(
		&fakeSessionLister{
			list: func(_ context.Context) ([]domain.SessionRecord, error) {
				return nil, errors.New("list failed")
			},
		},
		&fakeAgentResolver{},
		&fakeMetricsStore{},
		quietLogger(),
		time.Minute,
	)
	err := collector.poll(context.Background())
	if err == nil {
		t.Fatal("expected error from poll, got nil")
	}
}

func TestCollector_PollAccumulatesExistingMetrics(t *testing.T) {
	store := &fakeMetricsStore{
		getSessionMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return &domain.SessionMetrics{TotalInputTokens: 50, TotalOutputTokens: 25}, nil
		},
		recordUsage: func(_ context.Context, _ domain.AgentUsage) error {
			return nil
		},
		upsertCurrent: func(_ context.Context, sm domain.SessionMetrics) error {
			if sm.TotalInputTokens != 150 {
				t.Fatalf("TotalInputTokens = %d, want 150 (50 existing + 100 new)", sm.TotalInputTokens)
			}
			if sm.TotalOutputTokens != 75 {
				t.Fatalf("TotalOutputTokens = %d, want 75 (25 existing + 50 new)", sm.TotalOutputTokens)
			}
			return nil
		},
	}
	collector := NewCollector(
		&fakeSessionLister{
			list: func(_ context.Context) ([]domain.SessionRecord, error) {
				return []domain.SessionRecord{
					{
						ID: "sess-1", IsTerminated: false, Harness: "claude-code",
						UpdatedAt: time.Now().UTC(),
						Metadata:  domain.SessionMetadata{WorkspacePath: "/ws/sess-1"},
					},
				}, nil
			},
		},
		&fakeAgentResolver{
			agent: func(_ domain.AgentHarness) (ports.Agent, bool) {
				return &fakeAgent{
					provider: &fakeUsageProvider{
						sessionUsage: func(_ context.Context, ref ports.SessionRef) (*domain.AgentUsage, bool, error) {
							return &domain.AgentUsage{
								SessionID: "sess-1", InputTokens: 100, OutputTokens: 50,
								Model: "claude-sonnet-4-20250514",
							}, true, nil
						},
					},
				}, true
			},
		},
		store,
		quietLogger(),
		time.Minute,
	)
	err := collector.poll(context.Background())
	if err != nil {
		t.Fatalf("poll() = %v, want nil", err)
	}
}

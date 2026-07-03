package metrics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var errTest = errors.New("test error")

type fakeStore struct {
	recordUsage               func(ctx context.Context, usage domain.AgentUsage) error
	upsertCurrent             func(ctx context.Context, sm domain.SessionMetrics) error
	getSessionMetrics         func(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
	getSessionMetricsHistory  func(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error)
	listSessionMetricsCurrent func(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error)
}

func (f *fakeStore) RecordUsage(ctx context.Context, usage domain.AgentUsage) error {
	if f.recordUsage != nil {
		return f.recordUsage(ctx, usage)
	}
	return nil
}

func (f *fakeStore) UpsertSessionMetricsCurrent(ctx context.Context, sm domain.SessionMetrics) error {
	if f.upsertCurrent != nil {
		return f.upsertCurrent(ctx, sm)
	}
	return nil
}

func (f *fakeStore) GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error) {
	if f.getSessionMetrics != nil {
		return f.getSessionMetrics(ctx, sessionID)
	}
	return nil, nil
}

func (f *fakeStore) GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error) {
	if f.getSessionMetricsHistory != nil {
		return f.getSessionMetricsHistory(ctx, sessionID, since)
	}
	return nil, nil
}

func (f *fakeStore) ListSessionMetricsCurrentByIDs(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error) {
	if f.listSessionMetricsCurrent != nil {
		return f.listSessionMetricsCurrent(ctx, sessionIDs)
	}
	return nil, nil
}

func TestGetSessionMetrics_Found(t *testing.T) {
	store := &fakeStore{
		getSessionMetrics: func(_ context.Context, id string) (*domain.SessionMetrics, error) {
			return &domain.SessionMetrics{SessionID: id, TotalInputTokens: 100}, nil
		},
	}
	svc := NewService(store)
	sm, err := svc.GetSessionMetrics(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sm == nil || sm.SessionID != "sess-1" {
		t.Fatalf("got %+v, want sessionID=sess-1", sm)
	}
	if sm.TotalInputTokens != 100 {
		t.Fatalf("TotalInputTokens = %d, want 100", sm.TotalInputTokens)
	}
}

func TestGetSessionMetrics_NotFound(t *testing.T) {
	store := &fakeStore{
		getSessionMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return nil, nil
		},
	}
	svc := NewService(store)
	sm, err := svc.GetSessionMetrics(context.Background(), "sess-missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sm != nil {
		t.Fatalf("expected nil, got %+v", sm)
	}
}

func TestGetSessionMetrics_StoreError(t *testing.T) {
	store := &fakeStore{
		getSessionMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return nil, errTest
		},
	}
	svc := NewService(store)
	_, err := svc.GetSessionMetrics(context.Background(), "sess-err")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetSessionMetricsHistory_Empty(t *testing.T) {
	store := &fakeStore{
		getSessionMetricsHistory: func(_ context.Context, _ string, _ time.Time) ([]domain.SessionMetrics, error) {
			return []domain.SessionMetrics{}, nil
		},
	}
	svc := NewService(store)
	rows, err := svc.GetSessionMetricsHistory(context.Background(), "sess-1", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty, got %d rows", len(rows))
	}
}

func TestGetSessionMetricsHistory_Results(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{
		getSessionMetricsHistory: func(_ context.Context, _ string, _ time.Time) ([]domain.SessionMetrics, error) {
			return []domain.SessionMetrics{
				{SessionID: "sess-1", TotalInputTokens: 50, RecordedAt: now},
				{SessionID: "sess-1", TotalInputTokens: 100, RecordedAt: now.Add(time.Minute)},
			}, nil
		},
	}
	svc := NewService(store)
	rows, err := svc.GetSessionMetricsHistory(context.Background(), "sess-1", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestNewService_Logger(t *testing.T) {
	svc := NewService(&fakeStore{})
	if svc.log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

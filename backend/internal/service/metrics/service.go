package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type Store interface {
	RecordUsage(ctx context.Context, usage domain.AgentUsage) error
	UpsertSessionMetricsCurrent(ctx context.Context, sm domain.SessionMetrics) error
	GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
	GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error)
	ListSessionMetricsCurrentByIDs(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error)
}

type Service struct {
	store Store
	log   *slog.Logger
}

func NewService(store Store) *Service {
	return &Service{store: store, log: slog.With("svc", "metrics")}
}

func (s *Service) RecordUsage(ctx context.Context, usage domain.AgentUsage) error {
	return s.store.RecordUsage(ctx, usage)
}

func (s *Service) UpsertSessionMetricsCurrent(ctx context.Context, sm domain.SessionMetrics) error {
	return s.store.UpsertSessionMetricsCurrent(ctx, sm)
}

func (s *Service) GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error) {
	sm, err := s.store.GetSessionMetrics(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session metrics: %w", err)
	}
	return sm, nil
}

func (s *Service) GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error) {
	rows, err := s.store.GetSessionMetricsHistory(ctx, sessionID, since)
	if err != nil {
		return nil, fmt.Errorf("get session metrics history: %w", err)
	}
	return rows, nil
}

func (s *Service) ListSessionMetricsCurrentByIDs(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error) {
	return s.store.ListSessionMetricsCurrentByIDs(ctx, sessionIDs)
}

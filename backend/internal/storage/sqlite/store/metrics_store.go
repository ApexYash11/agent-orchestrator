package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func (s *Store) RecordUsage(ctx context.Context, usage domain.AgentUsage) error {
	id := fmt.Sprintf("%s-%d", usage.SessionID, usage.RecordedAt.UnixNano())
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := s.qw.InsertSessionMetric(ctx, gen.InsertSessionMetricParams{
		ID:           id,
		SessionID:    usage.SessionID,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		Cost:         nullFloat64(usage.Cost),
		Model:        sql.NullString{String: usage.Model, Valid: usage.Model != ""},
		ContextPct:   nullFloat64(usage.ContextPct),
		RetryCount:   usage.RetryCount,
		RecordedAt:   usage.RecordedAt,
	}); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

func (s *Store) UpsertSessionMetricsCurrent(ctx context.Context, sm domain.SessionMetrics) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.UpsertSessionMetricsCurrent(ctx, gen.UpsertSessionMetricsCurrentParams{
		SessionID:          sm.SessionID,
		TotalInputTokens:   sm.TotalInputTokens,
		TotalOutputTokens:  sm.TotalOutputTokens,
		EstimatedCost:      nullFloat64(sm.EstimatedCost),
		Model:              sql.NullString{String: sm.Model, Valid: sm.Model != ""},
		ContextUtilization: nullFloat64(sm.ContextUtilization),
		RetryCount:         sm.RetryCount,
		LastActivityAt:     nullString(sm.LastActivityAt.Format(time.RFC3339)),
		UpdatedAt:          sm.UpdatedAt,
	})
}

func (s *Store) GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error) {
	row, err := s.qr.GetSessionMetrics(ctx, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get session metrics: %w", err)
	}
	sm := currentToDomain(row)
	return &sm, nil
}

func (s *Store) GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error) {
	rows, err := s.qr.GetSessionMetricsHistory(ctx, gen.GetSessionMetricsHistoryParams{
		SessionID:  sessionID,
		RecordedAt: since,
	})
	if err != nil {
		return nil, fmt.Errorf("get session metrics history: %w", err)
	}
	result := make([]domain.SessionMetrics, 0, len(rows))
	for _, r := range rows {
		result = append(result, domain.SessionMetrics{
			SessionID:          r.SessionID,
			TotalInputTokens:   r.InputTokens,
			TotalOutputTokens:  r.OutputTokens,
			EstimatedCost:      r.Cost.Float64,
			Model:              r.Model.String,
			ContextUtilization: r.ContextPct.Float64,
			RetryCount:         r.RetryCount,
			RecordedAt:         r.RecordedAt,
		})
	}
	return result, nil
}

func (s *Store) ListSessionMetricsCurrentByIDs(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error) {
	rows, err := s.qr.ListSessionMetricsCurrentByIDs(ctx, sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("list session metrics by ids: %w", err)
	}
	result := make(map[string]domain.SessionMetrics, len(rows))
	for _, r := range rows {
		result[r.SessionID] = currentToDomain(r)
	}
	return result, nil
}

func currentToDomain(r gen.SessionMetricsCurrent) domain.SessionMetrics {
	sm := domain.SessionMetrics{
		SessionID:          r.SessionID,
		TotalInputTokens:   r.TotalInputTokens,
		TotalOutputTokens:  r.TotalOutputTokens,
		Model:              r.Model.String,
		ContextUtilization: r.ContextUtilization.Float64,
		RetryCount:         r.RetryCount,
	}
	if r.EstimatedCost.Valid {
		sm.EstimatedCost = r.EstimatedCost.Float64
	}
	if r.LastActivityAt.Valid {
		if t, err := time.Parse(time.RFC3339, r.LastActivityAt.String); err == nil {
			sm.LastActivityAt = t
		}
	}
	return sm
}

func nullFloat64(v float64) sql.NullFloat64 {
	if v == -1 {
		return sql.NullFloat64{Valid: false}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

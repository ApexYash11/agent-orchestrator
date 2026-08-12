package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

// MetricsService is the controller-facing metrics service contract.
type MetricsService interface {
	GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
	GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error)
	ListSessionMetricsCurrentByIDs(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error)
}

// MetricsController owns the /sessions/{sessionId}/metrics routes.
type MetricsController struct {
	Svc MetricsService
}

// Register mounts metric REST routes on the supplied router.
func (c *MetricsController) Register(r chi.Router) {
	r.Get("/sessions/{sessionId}/metrics", c.getMetrics)
	r.Get("/sessions/{sessionId}/metrics/history", c.getMetricsHistory)
}

func (c *MetricsController) getMetrics(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/sessions/{sessionId}/metrics")
		return
	}
	sm, err := c.Svc.GetSessionMetrics(r.Context(), string(sessionID(r)))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	if sm == nil {
		envelope.WriteJSON(w, http.StatusOK, GetSessionMetricsResponse{Metrics: nil})
		return
	}
	envelope.WriteJSON(w, http.StatusOK, GetSessionMetricsResponse{
		Metrics: newSessionMetricsDetail(sm),
	})
}

func (c *MetricsController) getMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/sessions/{sessionId}/metrics/history")
		return
	}
	since := parseMetricsSince(r)
	points, err := c.Svc.GetSessionMetricsHistory(r.Context(), string(sessionID(r)), since)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, GetSessionMetricsHistoryResponse{
		Metrics: newSessionMetricsPoints(points),
	})
}

func parseMetricsSince(r *http.Request) time.Time {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func newSessionMetricsDetail(sm *domain.SessionMetrics) *SessionMetricsDetail {
	return &SessionMetricsDetail{
		TotalInputTokens:   sm.TotalInputTokens,
		TotalOutputTokens:  sm.TotalOutputTokens,
		EstimatedCost:      sm.EstimatedCost,
		Model:              sm.Model,
		ContextUtilization: sm.ContextUtilization,
		RetryCount:         int(sm.RetryCount),
		LastActivityAt:     sm.LastActivityAt,
	}
}

func newSessionMetricsPoints(rows []domain.SessionMetrics) []SessionMetricsPoint {
	out := make([]SessionMetricsPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionMetricsPoint{
			InputTokens:  r.TotalInputTokens,
			OutputTokens: r.TotalOutputTokens,
			Cost:         r.EstimatedCost,
			RecordedAt:   r.RecordedAt,
		})
	}
	return out
}

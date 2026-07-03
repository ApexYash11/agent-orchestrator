package ports

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type MetricsReporter interface {
	RecordUsage(ctx context.Context, sessionID string, usage domain.AgentUsage) error
	GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
	GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error)
}

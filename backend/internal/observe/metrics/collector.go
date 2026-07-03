package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// SessionLister lists sessions for metrics collection.
type SessionLister interface {
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
}

// AgentResolver resolves a session's harness to its agent adapter.
type AgentResolver = ports.AgentResolver

// MetricsStore is the write surface the collector needs.
type MetricsStore interface {
	RecordUsage(ctx context.Context, usage domain.AgentUsage) error
	UpsertSessionMetricsCurrent(ctx context.Context, sm domain.SessionMetrics) error
	GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
}

// Collector polls active sessions for usage data.
type Collector struct {
	sessions SessionLister
	resolver ports.AgentResolver
	store    MetricsStore
	logger   *slog.Logger
	tick     time.Duration
}

// NewCollector creates a metrics collector.
func NewCollector(sessions SessionLister, resolver ports.AgentResolver, store MetricsStore, logger *slog.Logger, tick time.Duration) *Collector {
	return &Collector{
		sessions: sessions,
		resolver: resolver,
		store:    store,
		logger:   logger,
		tick:     tick,
	}
}

// Start begins the polling loop. Returns a channel that closes on shutdown.
func (c *Collector) Start(ctx context.Context) <-chan struct{} {
	return observe.StartPollLoop(ctx, c.tick, c.poll, c.logger, "metrics collector")
}

func (c *Collector) poll(ctx context.Context) error {
	records, err := c.sessions.ListAllSessions(ctx)
	if err != nil {
		return err
	}

	for _, rec := range records {
		if err := c.pollSession(ctx, rec); err != nil {
			c.logger.Error("metrics collector: session poll failed",
				"session_id", rec.ID,
				"err", err,
			)
		}
	}
	return nil
}

func (c *Collector) pollSession(ctx context.Context, rec domain.SessionRecord) error {
	if rec.IsTerminated {
		return nil
	}

	agent, ok := c.resolver.Agent(rec.Harness)
	if !ok {
		return nil
	}

	provider, ok := agent.(ports.UsageProvider)
	if !ok {
		return nil
	}

	sessionRef := ports.SessionRef{
		ID:            string(rec.ID),
		WorkspacePath: rec.Metadata.WorkspacePath,
		Metadata: map[string]string{
			ports.MetadataKeyAgentSessionID: rec.Metadata.AgentSessionID,
		},
	}

	usage, ok, err := provider.SessionUsage(ctx, sessionRef)
	if err != nil {
		return err
	}
	if !ok || usage == nil {
		return nil
	}

	usage.FillCost()

	if err := c.store.RecordUsage(ctx, *usage); err != nil {
		return err
	}

	now := time.Now().UTC()
	var lastActivity string
	if !rec.UpdatedAt.IsZero() {
		lastActivity = rec.UpdatedAt.Format(time.RFC3339)
	} else {
		lastActivity = now.Format(time.RFC3339)
	}
	lastTime, _ := time.Parse(time.RFC3339, lastActivity)

	sm := domain.SessionMetrics{
		SessionID:          usage.SessionID,
		TotalInputTokens:   usage.InputTokens,
		TotalOutputTokens:  usage.OutputTokens,
		EstimatedCost:      usage.Cost,
		Model:              usage.Model,
		ContextUtilization: usage.ContextPct,
		RetryCount:         usage.RetryCount,
		LastActivityAt:     lastTime,
		UpdatedAt:          now,
	}

	if existing, err := c.store.GetSessionMetrics(ctx, usage.SessionID); err == nil && existing != nil {
		sm.TotalInputTokens += existing.TotalInputTokens
		sm.TotalOutputTokens += existing.TotalOutputTokens
	}

	if err := c.store.UpsertSessionMetricsCurrent(ctx, sm); err != nil {
		return err
	}

	return nil
}

func (c *Collector) Shutdown(ctx context.Context) error {
	return nil
}

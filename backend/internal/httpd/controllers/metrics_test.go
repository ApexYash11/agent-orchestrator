package controllers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

type fakeMetricsService struct {
	getMetrics func(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
	getHistory func(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error)
	listByIDs  func(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error)
}

func (f *fakeMetricsService) GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error) {
	return f.getMetrics(ctx, sessionID)
}

func (f *fakeMetricsService) GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error) {
	return f.getHistory(ctx, sessionID, since)
}

func (f *fakeMetricsService) ListSessionMetricsCurrentByIDs(ctx context.Context, sessionIDs []string) (map[string]domain.SessionMetrics, error) {
	if f.listByIDs != nil {
		return f.listByIDs(ctx, sessionIDs)
	}
	return nil, nil
}

func TestMetricsAPI_GetMetrics_Found(t *testing.T) {
	svc := &fakeMetricsService{
		getMetrics: func(_ context.Context, id string) (*domain.SessionMetrics, error) {
			return &domain.SessionMetrics{
				SessionID: id, TotalInputTokens: 100, TotalOutputTokens: 50,
				EstimatedCost: 0.001, Model: "claude-sonnet-4-20250514",
				ContextUtilization: 0.45, RetryCount: 2,
			}, nil
		},
	}
	srv := newMetricsTestServer(t, svc)
	body, status, headers := doRequest(t, srv, "GET", "/api/v1/sessions/sess-1/metrics", "")
	if status != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200; body=%s", status, body)
	}
	assertJSON(t, headers)
	var resp controllers.GetSessionMetricsResponse
	mustJSON(t, body, &resp)
	if resp.Metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if resp.Metrics.TotalInputTokens != 100 {
		t.Fatalf("TotalInputTokens = %d, want 100", resp.Metrics.TotalInputTokens)
	}
	if resp.Metrics.TotalOutputTokens != 50 {
		t.Fatalf("TotalOutputTokens = %d, want 50", resp.Metrics.TotalOutputTokens)
	}
	if resp.Metrics.EstimatedCost != 0.001 {
		t.Fatalf("EstimatedCost = %f, want 0.001", resp.Metrics.EstimatedCost)
	}
}

func TestMetricsAPI_GetMetrics_Nil(t *testing.T) {
	svc := &fakeMetricsService{
		getMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return nil, nil
		},
	}
	srv := newMetricsTestServer(t, svc)
	body, status, headers := doRequest(t, srv, "GET", "/api/v1/sessions/sess-none/metrics", "")
	if status != http.StatusOK {
		t.Fatalf("GET /metrics (nil) = %d, want 200; body=%s", status, body)
	}
	assertJSON(t, headers)
	var resp controllers.GetSessionMetricsResponse
	mustJSON(t, body, &resp)
	if resp.Metrics != nil {
		t.Fatal("expected nil metrics for missing session")
	}
}

func TestMetricsAPI_GetMetrics_Error(t *testing.T) {
	svc := &fakeMetricsService{
		getMetrics: func(_ context.Context, _ string) (*domain.SessionMetrics, error) {
			return nil, errors.New("store error")
		},
	}
	srv := newMetricsTestServer(t, svc)
	_, status, _ := doRequest(t, srv, "GET", "/api/v1/sessions/sess-err/metrics", "")
	if status != http.StatusInternalServerError {
		t.Fatalf("GET /metrics with error = %d, want 500", status)
	}
}

func TestMetricsAPI_GetHistory_Empty(t *testing.T) {
	svc := &fakeMetricsService{
		getHistory: func(_ context.Context, _ string, _ time.Time) ([]domain.SessionMetrics, error) {
			return []domain.SessionMetrics{}, nil
		},
	}
	srv := newMetricsTestServer(t, svc)
	body, status, headers := doRequest(t, srv, "GET", "/api/v1/sessions/sess-1/metrics/history", "")
	if status != http.StatusOK {
		t.Fatalf("GET /history = %d, want 200; body=%s", status, body)
	}
	assertJSON(t, headers)
	var resp controllers.GetSessionMetricsHistoryResponse
	mustJSON(t, body, &resp)
	if len(resp.Metrics) != 0 {
		t.Fatalf("expected empty history, got %d points", len(resp.Metrics))
	}
}

func TestMetricsAPI_GetHistory_WithPoints(t *testing.T) {
	now := time.Now().UTC()
	svc := &fakeMetricsService{
		getHistory: func(_ context.Context, _ string, _ time.Time) ([]domain.SessionMetrics, error) {
			return []domain.SessionMetrics{
				{TotalInputTokens: 100, TotalOutputTokens: 50, EstimatedCost: 0.001, RecordedAt: now},
				{TotalInputTokens: 200, TotalOutputTokens: 100, EstimatedCost: 0.002, RecordedAt: now.Add(time.Minute)},
			}, nil
		},
	}
	srv := newMetricsTestServer(t, svc)
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/sessions/sess-1/metrics/history", "")
	if status != http.StatusOK {
		t.Fatalf("GET /history = %d, want 200; body=%s", status, body)
	}
	var resp controllers.GetSessionMetricsHistoryResponse
	mustJSON(t, body, &resp)
	if len(resp.Metrics) != 2 {
		t.Fatalf("expected 2 history points, got %d", len(resp.Metrics))
	}
	if resp.Metrics[0].InputTokens != 100 || resp.Metrics[0].OutputTokens != 50 {
		t.Fatalf("first point = %+v", resp.Metrics[0])
	}
}

func TestMetricsAPI_GetHistory_Error(t *testing.T) {
	svc := &fakeMetricsService{
		getHistory: func(_ context.Context, _ string, _ time.Time) ([]domain.SessionMetrics, error) {
			return nil, errors.New("store error")
		},
	}
	srv := newMetricsTestServer(t, svc)
	_, status, _ := doRequest(t, srv, "GET", "/api/v1/sessions/sess-err/metrics/history", "")
	if status != http.StatusInternalServerError {
		t.Fatalf("GET /history with error = %d, want 500", status)
	}
}

func TestMetricsAPI_GetMetrics_NilSvcReturns501(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/sessions/sess-1/metrics", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestMetricsAPI_GetHistory_NilSvcReturns501(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/sessions/sess-1/metrics/history", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestMetricsAPI_HistoryWithSince(t *testing.T) {
	now := time.Now().UTC()
	svc := &fakeMetricsService{
		getHistory: func(_ context.Context, _ string, since time.Time) ([]domain.SessionMetrics, error) {
			if since.IsZero() {
				t.Fatal("expected non-zero since")
			}
			return []domain.SessionMetrics{
				{TotalInputTokens: 300, TotalOutputTokens: 150, EstimatedCost: 0.003, RecordedAt: now},
			}, nil
		},
	}
	srv := newMetricsTestServer(t, svc)
	_, status, _ := doRequest(t, srv, "GET", "/api/v1/sessions/sess-1/metrics/history?since="+now.Add(-time.Hour).Format(time.RFC3339), "")
	if status != http.StatusOK {
		t.Fatalf("GET /history with since = %d, want 200", status)
	}
}

func newMetricsTestServer(t *testing.T, svc controllers.MetricsService) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Metrics: svc}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

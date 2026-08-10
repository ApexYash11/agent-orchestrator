package review

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
)

func TestParseProviderReviewMarker(t *testing.T) {
	body := "Looks good.\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved -->"

	marker, prose, ok := parseProviderReviewMarker(body)

	if !ok {
		t.Fatal("expected marker to parse")
	}
	if marker.RunID != "run-123" || marker.SHA != "abc123" || marker.Verdict != domain.VerdictApproved {
		t.Fatalf("marker = %#v", marker)
	}
	if prose != "Looks good." {
		t.Fatalf("prose = %q", prose)
	}
}

func TestReconcileProviderReviewsCompletesMatchingRunningRun(t *testing.T) {
	store := &fakeStore{
		ok: true,
		run: domain.ReviewRun{
			ID: "run-123", SessionID: "worker-1", PRURL: "https://github.com/o/r/pull/7",
			TargetSHA: "abc123", Status: domain.ReviewRunRunning,
		},
	}
	service := New(nil, store)
	pr := domain.PullRequest{URL: store.run.PRURL, HeadSHA: store.run.TargetSHA}
	reviews := []domain.PullRequestReview{{
		ID:   "98765",
		Body: "Please fix the race.\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=changes_requested -->",
	}}

	if err := service.ReconcileProviderReviews(context.Background(), "worker-1", pr, reviews); err != nil {
		t.Fatalf("ReconcileProviderReviews: %v", err)
	}
	if store.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", store.updateCalls)
	}
	if store.run.Status != domain.ReviewRunComplete || store.run.Verdict != domain.VerdictChangesRequested {
		t.Fatalf("run status/verdict = %q/%q", store.run.Status, store.run.Verdict)
	}
	if store.run.Body != "Please fix the race." || store.run.GithubReviewID != "98765" {
		t.Fatalf("run body/id = %q/%q", store.run.Body, store.run.GithubReviewID)
	}
}

func TestReconcileProviderReviewsRetriesDeliveryForCompletedRun(t *testing.T) {
	store := &fakeStore{
		ok: true,
		run: domain.ReviewRun{
			ID: "run-123", SessionID: "worker-1", BatchID: "batch-1",
			PRURL: "https://github.com/o/r/pull/7", TargetSHA: "abc123",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictChangesRequested,
			Body: "stored prose", GithubReviewID: "98765", AutoInjectReview: true,
		},
		prs: []domain.PullRequest{{URL: "https://github.com/o/r/pull/7", HeadSHA: "abc123"}},
	}
	reducer := &fakeReducer{outcome: lifecycle.ReviewDeliverySent}
	service := New(nil, store, WithLifecycleReducer(reducer))
	reviews := []domain.PullRequestReview{{
		ID:   "98765",
		Body: "provider prose\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=changes_requested -->",
	}}

	err := service.ReconcileProviderReviews(context.Background(), "worker-1", store.prs[0], reviews)
	if err != nil {
		t.Fatalf("ReconcileProviderReviews: %v", err)
	}
	if store.updateCalls != 0 || reducer.batchCalls != 1 || store.run.Status != domain.ReviewRunDelivered {
		t.Fatalf("update/delivery/status = %d/%d/%q", store.updateCalls, reducer.batchCalls, store.run.Status)
	}
	if got := reducer.gotBatch[0].Body; got != "stored prose" {
		t.Fatalf("delivered body = %q, want stored prose", got)
	}
}

func TestReconcileProviderReviewsIgnoresMismatchedFacts(t *testing.T) {
	baseRun := domain.ReviewRun{
		ID: "run-123", SessionID: "worker-1", PRURL: "https://github.com/o/r/pull/7",
		TargetSHA: "abc123", Status: domain.ReviewRunRunning,
	}
	basePR := domain.PullRequest{URL: baseRun.PRURL, HeadSHA: baseRun.TargetSHA}
	baseReview := domain.PullRequestReview{
		ID:   "98765",
		Body: "prose\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved -->",
	}
	tests := map[string]func(*domain.ReviewRun, *domain.PullRequest, *domain.PullRequestReview, *domain.SessionID){
		"session": func(_ *domain.ReviewRun, _ *domain.PullRequest, _ *domain.PullRequestReview, worker *domain.SessionID) {
			*worker = "worker-2"
		},
		"pr url": func(_ *domain.ReviewRun, pr *domain.PullRequest, _ *domain.PullRequestReview, _ *domain.SessionID) {
			pr.URL += "-other"
		},
		"head sha": func(_ *domain.ReviewRun, pr *domain.PullRequest, _ *domain.PullRequestReview, _ *domain.SessionID) {
			pr.HeadSHA = "def456"
		},
		"review id": func(_ *domain.ReviewRun, _ *domain.PullRequest, review *domain.PullRequestReview, _ *domain.SessionID) {
			review.ID = "opaque"
		},
		"malformed marker": func(_ *domain.ReviewRun, _ *domain.PullRequest, review *domain.PullRequestReview, _ *domain.SessionID) {
			review.Body = "prose only"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			run, pr, observed, worker := baseRun, basePR, baseReview, domain.SessionID("worker-1")
			mutate(&run, &pr, &observed, &worker)
			store := &fakeStore{ok: true, run: run}
			if err := New(nil, store).ReconcileProviderReviews(context.Background(), worker, pr, []domain.PullRequestReview{observed}); err != nil {
				t.Fatalf("ReconcileProviderReviews: %v", err)
			}
			if store.updateCalls != 0 {
				t.Fatalf("update calls = %d, want 0", store.updateCalls)
			}
		})
	}
}

func TestReconcileProviderReviewsIgnoresCompletedRunWithDifferentReviewID(t *testing.T) {
	store := &fakeStore{ok: true, run: domain.ReviewRun{
		ID: "run-123", SessionID: "worker-1", PRURL: "pr-1", TargetSHA: "abc123",
		Status: domain.ReviewRunComplete, Verdict: domain.VerdictChangesRequested, Body: "stored", GithubReviewID: "98765", AutoInjectReview: true,
	}, prs: []domain.PullRequest{{URL: "pr-1", HeadSHA: "abc123"}}}
	reducer := &fakeReducer{outcome: lifecycle.ReviewDeliverySent}
	observed := domain.PullRequestReview{ID: "12345", Body: "prose\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=changes_requested -->"}

	err := New(nil, store, WithLifecycleReducer(reducer)).ReconcileProviderReviews(
		context.Background(), "worker-1", domain.PullRequest{URL: "pr-1", HeadSHA: "abc123"}, []domain.PullRequestReview{observed},
	)
	if err != nil {
		t.Fatalf("ReconcileProviderReviews: %v", err)
	}
	if reducer.batchCalls != 0 {
		t.Fatalf("delivery calls = %d, want 0", reducer.batchCalls)
	}
}

func TestParseProviderReviewMarkerRejectsMalformedBodies(t *testing.T) {
	tests := map[string]string{
		"unknown version":  "Looks good.\n\n<!-- ao-review:v2 run=run-123 sha=abc123 verdict=approved -->",
		"missing field":    "Looks good.\n\n<!-- ao-review:v1 run=run-123 verdict=approved -->",
		"extra field":      "Looks good.\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved extra=x -->",
		"duplicate marker": "Looks good.\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved -->\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved -->",
		"invalid verdict":  "Looks good.\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=commented -->",
		"empty prose":      "<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved -->",
		"suffix text":      "Looks good.\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved --> trailing",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if marker, prose, ok := parseProviderReviewMarker(body); ok {
				t.Fatalf("unexpected parse: marker=%#v prose=%q", marker, prose)
			}
		})
	}
}

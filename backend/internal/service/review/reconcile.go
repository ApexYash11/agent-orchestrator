package review

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var providerReviewMarkerRE = regexp.MustCompile(`(?s)^(.*?)\s*<!-- ao-review:v1 run=([A-Za-z0-9._:-]+) sha=([A-Fa-f0-9]+) verdict=(approved|changes_requested) -->\s*$`)

type providerReviewMarker struct {
	RunID   string
	SHA     string
	Verdict domain.ReviewVerdict
}

// ReconcileProviderReviews completes marked runs from provider facts when the
// reviewer posted to GitHub but could not execute the explicit AO submit step.
func (s *Service) ReconcileProviderReviews(ctx context.Context, workerID domain.SessionID, pr domain.PullRequest, reviews []domain.PullRequestReview) error {
	for _, observed := range reviews {
		marker, prose, ok := parseProviderReviewMarker(observed.Body)
		if !ok || !isPositiveDecimalID(observed.ID) {
			continue
		}
		run, found, err := s.store.GetReviewRun(ctx, marker.RunID)
		if err != nil {
			return fmt.Errorf("get marked review run: %w", err)
		}
		if !found || run.SessionID != workerID || run.PRURL != pr.URL || marker.SHA != run.TargetSHA || marker.SHA != pr.HeadSHA {
			continue
		}
		if run.Status == domain.ReviewRunDelivered {
			continue
		}

		verdict, body, reviewID := marker.Verdict, prose, observed.ID
		if run.Status == domain.ReviewRunComplete {
			if observed.ID != run.GithubReviewID {
				continue
			}
			verdict, body, reviewID = run.Verdict, run.Body, run.GithubReviewID
		}
		if verdict != marker.Verdict || !isPositiveDecimalID(reviewID) {
			continue
		}
		if _, err := s.Submit(ctx, workerID, run.ID, verdict, body, reviewID); err != nil {
			if errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) {
				continue
			}
			return fmt.Errorf("submit reconciled provider review: %w", err)
		}
	}
	return nil
}

func isPositiveDecimalID(value string) bool {
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id > 0
}

func parseProviderReviewMarker(body string) (providerReviewMarker, string, bool) {
	match := providerReviewMarkerRE.FindStringSubmatch(body)
	if match == nil || strings.Count(body, "<!-- ao-review:") != 1 {
		return providerReviewMarker{}, "", false
	}
	prose := strings.TrimSpace(match[1])
	if prose == "" {
		return providerReviewMarker{}, "", false
	}
	return providerReviewMarker{
		RunID:   match[2],
		SHA:     match[3],
		Verdict: domain.ReviewVerdict(match[4]),
	}, prose, true
}

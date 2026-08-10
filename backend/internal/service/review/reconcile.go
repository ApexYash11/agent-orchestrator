package review

import (
	"regexp"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var providerReviewMarkerRE = regexp.MustCompile(`(?s)^(.*?)\s*<!-- ao-review:v1 run=([A-Za-z0-9._:-]+) sha=([A-Fa-f0-9]+) verdict=(approved|changes_requested) -->\s*$`)

type providerReviewMarker struct {
	RunID   string
	SHA     string
	Verdict domain.ReviewVerdict
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

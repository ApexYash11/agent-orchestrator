package review

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
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

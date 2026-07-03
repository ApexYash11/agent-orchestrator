package domain

import "testing"

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestComputeCost_ClaudeSonnet4(t *testing.T) {
	cost, ok := ComputeCost("claude-sonnet-4-20250514", 1000, 500)
	if !ok {
		t.Fatal("ComputeCost should succeed for claude-sonnet-4")
	}
	expected := 1000*3.0/1_000_000 + 500*15.0/1_000_000
	if abs(cost-expected) > 1e-9 {
		t.Fatalf("cost = %g, want %g", cost, expected)
	}
}

func TestComputeCost_ClaudeOpus45(t *testing.T) {
	cost, ok := ComputeCost("claude-opus-4-5", 500, 200)
	if !ok {
		t.Fatal("ComputeCost should succeed for claude-opus-4-5")
	}
	expected := 500*15.0/1_000_000 + 200*75.0/1_000_000
	if cost != expected {
		t.Fatalf("cost = %f, want %f", cost, expected)
	}
}

func TestComputeCost_ClaudeHaiku45(t *testing.T) {
	cost, ok := ComputeCost("claude-haiku-4-5", 2000, 1000)
	if !ok {
		t.Fatal("ComputeCost should succeed for claude-haiku-4-5")
	}
	expected := 2000*0.80/1_000_000 + 1000*4.00/1_000_000
	if cost != expected {
		t.Fatalf("cost = %f, want %f", cost, expected)
	}
}

func TestComputeCost_GPT4o(t *testing.T) {
	cost, ok := ComputeCost("gpt-4o", 500, 200)
	if !ok {
		t.Fatal("ComputeCost should succeed for gpt-4o")
	}
	expected := 500*2.5/1_000_000 + 200*10.0/1_000_000
	if abs(cost-expected) > 1e-9 {
		t.Fatalf("cost = %g, want %g", cost, expected)
	}
}

func TestComputeCost_UnknownModel(t *testing.T) {
	_, ok := ComputeCost("unknown-model", 100, 50)
	if ok {
		t.Fatal("ComputeCost should fail for unknown model")
	}
}

func TestComputeCost_EmptyModel(t *testing.T) {
	_, ok := ComputeCost("", 100, 50)
	if ok {
		t.Fatal("ComputeCost should fail for empty model")
	}
}

func TestComputeCost_ZeroTokens(t *testing.T) {
	cost, ok := ComputeCost("claude-sonnet-4-20250514", 0, 0)
	if !ok {
		t.Fatal("ComputeCost should succeed for zero tokens")
	}
	if cost != 0 {
		t.Fatalf("cost = %f, want 0", cost)
	}
}

func TestDefaultPricing_AllModelsPresent(t *testing.T) {
	models := []string{
		"claude-sonnet-4-20250514",
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-haiku-4-5",
		"gpt-4o",
		"gpt-4o-mini",
	}
	for _, m := range models {
		t.Run(m, func(t *testing.T) {
			_, ok := ComputeCost(m, 100, 50)
			if !ok {
				t.Fatalf("ComputeCost should find pricing for %q", m)
			}
		})
	}
}

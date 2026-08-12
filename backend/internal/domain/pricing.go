package domain

type ModelPricing struct {
	Input  float64
	Output float64
}

var DefaultPricing = map[string]ModelPricing{
	"claude-sonnet-4-20250514": {Input: 3.00, Output: 15.00},
	"claude-opus-4-5":          {Input: 15.00, Output: 75.00},
	"claude-sonnet-4-5":        {Input: 3.00, Output: 15.00},
	"claude-haiku-4-5":         {Input: 0.80, Output: 4.00},
	"gpt-4o":                   {Input: 2.50, Output: 10.00},
	"gpt-4o-mini":              {Input: 0.15, Output: 0.60},
}

func ComputeCost(model string, inputTokens, outputTokens int64) (float64, bool) {
	pricing, ok := DefaultPricing[model]
	if !ok {
		return 0, false
	}
	return (float64(inputTokens)/1_000_000)*pricing.Input + (float64(outputTokens)/1_000_000)*pricing.Output, true
}

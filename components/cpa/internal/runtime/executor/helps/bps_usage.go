package helps

import (
	"context"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

// BPSUsageDetail keeps missing per-generation fields nil in the existing iteration ledger.
func BPSUsageDetail(ctx context.Context, event []byte) usage.Detail {
	detail, _ := ParseCodexUsage(event)
	item := usage.IterationUsage{Type: "bps_initial", Model: ojson.GetBytes(event, "response.model").String()}
	if coreauth.BPSGenerationCount(ctx) == 2 {
		item.Type = "bps_failover"
	}
	fields := []struct {
		path string
		dest **int64
	}{
		{"input_tokens", &item.InputTokens}, {"output_tokens", &item.OutputTokens},
		{"output_tokens_details.reasoning_tokens", &item.ReasoningTokens},
		{"input_tokens_details.cached_tokens", &item.CacheReadInputTokens},
	}
	for _, field := range fields {
		value := ojson.GetBytes(event, "response.usage."+field.path)
		if value.Type == ojson.Number && value.Int() >= 0 {
			n := value.Int()
			*field.dest = &n
		}
	}
	detail.Iterations = usage.IterationsPtr([]usage.IterationUsage{item})
	return detail
}

func BPSWithIterations(detail usage.Detail, iterations []usage.IterationUsage) usage.Detail {
	if len(iterations) == 0 {
		return detail
	}
	detail.Iterations = usage.IterationsPtr(append([]usage.IterationUsage(nil), iterations...))
	detail.InputTokens, detail.OutputTokens, detail.ReasoningTokens, detail.CachedTokens, detail.CacheReadTokens = 0, 0, 0, 0, 0
	for _, item := range iterations {
		if item.InputTokens != nil {
			detail.InputTokens += *item.InputTokens
		}
		if item.OutputTokens != nil {
			detail.OutputTokens += *item.OutputTokens
		}
		if item.ReasoningTokens != nil {
			detail.ReasoningTokens += *item.ReasoningTokens
		}
		if item.CacheReadInputTokens != nil {
			detail.CacheReadTokens += *item.CacheReadInputTokens
		}
	}
	detail.CachedTokens = detail.CacheReadTokens
	detail.TotalTokens = detail.InputTokens + detail.OutputTokens
	return detail
}

func BPSResponseIteration(ctx context.Context, response map[string]any, correction bool) usage.IterationUsage {
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("response", response)
	detail := BPSUsageDetail(ctx, doc.Bytes())
	item := (*detail.Iterations)[0]
	if correction {
		item.Type = "bps_correction"
	}
	return item
}

// BPSAggregatedResponseUsage sums reported counters and leaves unknown counters absent.
func BPSAggregatedResponseUsage(iterations []usage.IterationUsage) map[string]any {
	result := map[string]any{}
	var input, output, reasoning, cached int64
	var hasInput, hasOutput, hasReasoning, hasCached bool
	for _, item := range iterations {
		if item.InputTokens != nil {
			input += *item.InputTokens
			hasInput = true
		}
		if item.OutputTokens != nil {
			output += *item.OutputTokens
			hasOutput = true
		}
		if item.ReasoningTokens != nil {
			reasoning += *item.ReasoningTokens
			hasReasoning = true
		}
		if item.CacheReadInputTokens != nil {
			cached += *item.CacheReadInputTokens
			hasCached = true
		}
	}
	if hasInput {
		result["input_tokens"] = input
	}
	if hasOutput {
		result["output_tokens"] = output
	}
	if hasInput && hasOutput {
		result["total_tokens"] = input + output
	}
	if hasReasoning {
		result["output_tokens_details"] = map[string]any{"reasoning_tokens": reasoning}
	}
	if hasCached {
		result["input_tokens_details"] = map[string]any{"cached_tokens": cached}
	}
	return result
}

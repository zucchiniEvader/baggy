package catalog

import (
	"math"
	"testing"

	"github.com/zucchiniEvader/baggy/agent"
)

func TestFindAndCost(t *testing.T) {
	m, ok := Find("anthropic/claude-sonnet-5-5")
	if !ok || m.ContextWindow != 1000000 || !m.Images {
		t.Fatalf("sonnet: %+v %v", m, ok)
	}
	if _, ok := Find("claude-haiku-4-5-20251001"); !ok {
		t.Fatal("dated id not found")
	}
	if _, ok := Find("claude-haiku-4-5-x"); ok {
		t.Fatal("non-date suffix matched")
	}
	got := m.Cost(agent.Usage{Input: 1_000_000, Output: 100_000, CacheRead: 1_000_000})
	if math.Abs(got-(2.0+1.0+0.1)) > 1e-9 {
		t.Fatalf("cost %v", got)
	}
	h, _ := Find("claude-haiku-5-5")
	if low, high := h.Cost(agent.Usage{Input: 100_000}), h.Cost(agent.Usage{Input: 100_001}); low != 0.01 || high <= low*4 {
		t.Fatalf("tiers: %v %v", low, high)
	}
	if Window("unknown-model") != DefaultWindow || !Images("unknown-model") || Images("deepseek-v4-pro") {
		t.Fatal("fallbacks")
	}
}

package autorouter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectTier(t *testing.T) {
	policy := Policy{DefaultTier: TierMid, UpgradeBenefit: 0.5, MinTaskTypeProbability: 0.5, ContinuationTaskChange: 0.5}
	p := func(v float64) *float64 { return &v }
	tests := []struct {
		name          string
		decision      Decision
		previousTier  string
		wantTier      string
		wantInherited bool
	}{
		{"simple task", Decision{TaskType: "direct_qa", TaskTypeProbability: 0.9, Difficulty: 1, TaskChange: p(1), LargerModelBenefit: p(0.1)}, "", TierLow, false},
		{"medium task", Decision{TaskType: "coding", TaskTypeProbability: 0.9, Difficulty: 2, TaskChange: p(1), LargerModelBenefit: p(0.1)}, "", TierMid, false},
		{"very hard task", Decision{TaskType: "operations", TaskTypeProbability: 0.9, Difficulty: 4, TaskChange: p(1), LargerModelBenefit: p(0.1)}, "", TierHigh, false},
		{"larger model benefit upgrades", Decision{TaskType: "debugging", TaskTypeProbability: 0.9, Difficulty: 1, TaskChange: p(1), LargerModelBenefit: p(0.7)}, "", TierMid, false},
		{"upgrade caps at high", Decision{TaskType: "debugging", TaskTypeProbability: 0.9, Difficulty: 3, TaskChange: p(1), LargerModelBenefit: p(0.9)}, "", TierHigh, false},
		{"continuation never downgrades", Decision{TaskType: "review", TaskTypeProbability: 0.9, Difficulty: 1, TaskChange: p(0.1), LargerModelBenefit: p(0.1)}, TierHigh, TierHigh, false},
		{"new task may downgrade", Decision{TaskType: "direct_qa", TaskTypeProbability: 0.9, Difficulty: 1, TaskChange: p(0.9), LargerModelBenefit: p(0.1)}, TierHigh, TierLow, false},
		{"continuation may upgrade", Decision{TaskType: "debugging", TaskTypeProbability: 0.9, Difficulty: 4, TaskChange: p(0.1), LargerModelBenefit: p(0.1)}, TierLow, TierHigh, false},
		{"unknown inherits previous", Decision{TaskType: TaskTypeUnknown, TaskTypeProbability: 0.9, Difficulty: 1}, TierHigh, TierHigh, true},
		{"unknown without previous uses default", Decision{TaskType: TaskTypeUnknown, TaskTypeProbability: 0.9, Difficulty: 1}, "", TierMid, true},
		{"low confidence inherits previous", Decision{TaskType: "coding", TaskTypeProbability: 0.3, Difficulty: 4}, TierLow, TierLow, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier, inherited := SelectTier(tt.decision, tt.previousTier, policy)
			assert.Equal(t, tt.wantTier, tier)
			assert.Equal(t, tt.wantInherited, inherited)
		})
	}
}

func TestPickModel(t *testing.T) {
	tiers := map[string][]string{
		TierLow:  {"low-a", "low-b"},
		TierMid:  {"mid-a"},
		TierHigh: {"high-a"},
	}
	tests := []struct {
		name      string
		tier      string
		available []string
		wantModel string
		wantTier  string
		wantOK    bool
	}{
		{"first available in tier", TierLow, []string{"low-b", "low-a", "mid-a"}, "low-a", TierLow, true},
		{"skips unavailable", TierLow, []string{"low-b", "mid-a"}, "low-b", TierLow, true},
		{"empty tier prefers higher on tie", TierMid, []string{"low-a", "high-a"}, "high-a", TierHigh, true},
		{"empty tier falls to nearest", TierHigh, []string{"low-a", "mid-a"}, "mid-a", TierMid, true},
		{"walks two rungs", TierLow, []string{"high-a"}, "high-a", TierHigh, true},
		{"nothing available", TierMid, nil, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, tier, ok := PickModel(tt.tier, tiers, func(m string) bool {
				for _, a := range tt.available {
					if a == m {
						return true
					}
				}
				return false
			})
			assert.Equal(t, tt.wantModel, model)
			assert.Equal(t, tt.wantTier, tier)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

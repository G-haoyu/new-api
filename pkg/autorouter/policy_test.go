package autorouter

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectRoute(t *testing.T) {
	policy := Policy{
		TaskModels: map[string][]string{
			"coding":    {"coder", "coder-b"},
			"direct_qa": {"chat"},
			"review":    {"gone"},
		},
		DefaultModels:          []string{"general"},
		DifficultyEfforts:      []string{"low", "medium", "high", "xhigh"},
		DefaultEffort:          "medium",
		UpgradeBenefit:         0.5,
		MinTaskTypeProbability: 0.5,
		ContinuationTaskChange: 0.5,
	}
	p := func(v float64) *float64 { return &v }
	decide := func(taskType string, difficulty int, taskChange, benefit float64) *Decision {
		return &Decision{TaskType: taskType, TaskTypeProbability: 0.9, Difficulty: difficulty, TaskChange: p(taskChange), LargerModelBenefit: p(benefit)}
	}
	coderHigh := &Route{TaskType: "coding", Model: "coder", Effort: "high"}
	tests := []struct {
		name          string
		decision      *Decision
		previous      *Route
		available     []string
		want          Route
		wantInherited bool
		wantOK        bool
	}{
		{"task picks model, difficulty picks effort", decide("coding", 2, 1, 0.1), nil, nil, Route{TaskType: "coding", Model: "coder", Effort: "medium"}, false, true},
		{"simple question", decide("direct_qa", 1, 1, 0.1), nil, nil, Route{TaskType: "direct_qa", Model: "chat", Effort: "low"}, false, true},
		{"larger model benefit raises effort", decide("coding", 3, 1, 0.7), nil, nil, Route{TaskType: "coding", Model: "coder", Effort: "xhigh"}, false, true},
		{"rung past the table uses the last effort", decide("coding", 6, 1, 0.1), nil, nil, Route{TaskType: "coding", Model: "coder", Effort: "xhigh"}, false, true},
		{"skips unavailable task model", decide("coding", 1, 1, 0.1), nil, []string{"coder-b-low", "general"}, Route{TaskType: "coding", Model: "coder-b", Effort: "low"}, false, true},
		{"unconfigured task type uses defaults", decide("research", 4, 1, 0.1), nil, nil, Route{Model: "general", Effort: "xhigh"}, false, true},
		{"unavailable task models use defaults", decide("review", 1, 1, 0.1), nil, []string{"general"}, Route{Model: "general", Effort: "low"}, false, true},
		{"continuation keeps model and effort", decide("direct_qa", 1, 0.1, 0.1), coderHigh, nil, *coderHigh, false, true},
		{"continuation may raise effort", decide("debugging", 4, 0.1, 0.1), coderHigh, nil, Route{TaskType: "coding", Model: "coder", Effort: "xhigh"}, false, true},
		{"new task switches model", decide("direct_qa", 1, 0.9, 0.1), coderHigh, nil, Route{TaskType: "direct_qa", Model: "chat", Effort: "low"}, false, true},
		{"continuation leaves an unavailable model", decide("direct_qa", 1, 0.1, 0.1), coderHigh, []string{"chat"}, Route{TaskType: "direct_qa", Model: "chat", Effort: "low"}, false, true},
		{"unknown inherits previous", decide(TaskTypeUnknown, 1, 0.1, 0.1), coderHigh, nil, *coderHigh, true, true},
		{"low probability inherits previous", &Decision{TaskType: "direct_qa", TaskTypeProbability: 0.3, Difficulty: 1}, coderHigh, nil, *coderHigh, true, true},
		{"jev failure without previous uses defaults", nil, nil, nil, Route{Model: "general", Effort: "medium"}, true, true},
		{"unavailable previous uses defaults", nil, coderHigh, []string{"general"}, Route{Model: "general", Effort: "medium"}, true, true},
		{"nothing available", decide("coding", 1, 1, 0.1), nil, []string{}, Route{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available := func(model string) bool { return tt.available == nil || slices.Contains(tt.available, model) }
			route, inherited, ok := SelectRoute(tt.decision, tt.previous, policy, available, available)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.want, route)
			}
			assert.Equal(t, tt.wantInherited, inherited)
		})
	}
}

// 可信的判断自己定档位、会改写客户端的推理字段，所以用 overridable 判定候选；
// 兜底路径不碰客户端字段，仍按客户端原样的请求用 available 判定。
func TestSelectRouteJudgesTrustedAndFallbackCandidatesSeparately(t *testing.T) {
	policy := Policy{
		TaskModels:             map[string][]string{"coding": {"coder", "coder-b"}},
		DefaultModels:          []string{"general", "general-b"},
		DifficultyEfforts:      []string{"low", "medium", "high", "xhigh"},
		DefaultEffort:          "medium",
		UpgradeBenefit:         0.5,
		MinTaskTypeProbability: 0.5,
		ContinuationTaskChange: 0.5,
	}
	one := 1.0
	zero := 0.0
	trusted := &Decision{TaskType: "coding", TaskTypeProbability: 0.9, Difficulty: 2, TaskChange: &one, LargerModelBenefit: &zero}
	// 客户端自己带的推理档位 coder 的渠道不支持，但 auto 会把它改写掉，所以 coder 仍可选。
	available := func(model string) bool { return model == "coder-b" || model == "general-b" }
	overridable := func(string) bool { return true }

	route, inherited, ok := SelectRoute(trusted, nil, policy, available, overridable)
	assert.True(t, ok)
	assert.False(t, inherited)
	assert.Equal(t, "coder", route.Model, "可信判断按 overridable 选模")

	route, inherited, ok = SelectRoute(nil, nil, policy, available, overridable)
	assert.True(t, ok)
	assert.True(t, inherited)
	assert.Equal(t, "general-b", route.Model, "兜底按客户端原样的请求选模，跳过 general")
}

func TestPolicyAnyModelAvailable(t *testing.T) {
	policy := Policy{
		TaskModels:    map[string][]string{"coding": {"coder"}, "review": {"gone"}},
		DefaultModels: []string{"general"},
	}
	tests := []struct {
		name      string
		available []string
		want      bool
	}{
		{"a default model is enough", []string{"general"}, true},
		{"a task model is enough", []string{"coder-high"}, true},
		{"only models outside both lists", []string{"other"}, false},
		{"nothing available", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policy.AnyModelAvailable(func(model string) bool { return slices.Contains(tt.available, model) }))
		})
	}
}

func TestResolveVariant(t *testing.T) {
	tests := []struct {
		name      string
		effort    string
		available []string
		want      string
		wantOK    bool
	}{
		{"exact variant", "high", []string{"m", "m-high"}, "m-high", true},
		{"bare model is the provider default effort", "high", []string{"m", "m-low"}, "m", true},
		{"nearest variant", "xhigh", []string{"m-high", "m-low"}, "m-high", true},
		{"tie prefers stronger", "medium", []string{"m-low", "m-high"}, "m-high", true},
		{"walks several rungs", "none", []string{"m-max"}, "m-max", true},
		{"unknown effort searches from medium", "", []string{"m-low", "m-xhigh"}, "m-low", true},
		{"unknown effort finds a model listed only as medium", "", []string{"m-medium"}, "m-medium", true},
		{"unknown effort prefers medium over its neighbours", "", []string{"m-low", "m-medium", "m-high"}, "m-medium", true},
		{"other models do not count", "high", []string{"other-high"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, ok := ResolveVariant("m", tt.effort, func(name string) bool { return slices.Contains(tt.available, name) })
			assert.Equal(t, tt.want, model)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

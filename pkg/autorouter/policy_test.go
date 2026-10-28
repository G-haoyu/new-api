package autorouter

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectRoute(t *testing.T) {
	policy := Policy{
		TaskModels: map[string][]string{
			"code.build":   {"coder", "coder-b"},
			"code":         {"code-class"},
			"general.chat": {"chat"},
			"code.ops":     {"gone"},
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
	// spread splits the probability over task types; the chosen one is the
	// most likely.
	spread := func(probabilities map[string]float64) *Decision {
		d := &Decision{Difficulty: 1, TaskChange: p(1), TaskTypeProbabilities: probabilities}
		for taskType, probability := range probabilities {
			if probability > d.TaskTypeProbability {
				d.TaskType, d.TaskTypeProbability = taskType, probability
			}
		}
		return d
	}
	coderHigh := &Route{Profile: "code.build", Model: "coder", Effort: "high"}
	tests := []struct {
		name          string
		decision      *Decision
		previous      *Route
		available     []string
		want          Route
		wantInherited bool
		wantOK        bool
	}{
		{"task picks model, difficulty picks effort", decide("code.build", 2, 1, 0.1), nil, nil, Route{Profile: "code.build", Model: "coder", Effort: "medium"}, false, true},
		{"simple question", decide("general.chat", 1, 1, 0.1), nil, nil, Route{Profile: "general.chat", Model: "chat", Effort: "low"}, false, true},
		{"larger model benefit raises effort", decide("code.build", 3, 1, 0.7), nil, nil, Route{Profile: "code.build", Model: "coder", Effort: "xhigh"}, false, true},
		{"rung past the table uses the last effort", decide("code.build", 6, 1, 0.1), nil, nil, Route{Profile: "code.build", Model: "coder", Effort: "xhigh"}, false, true},
		{"skips unavailable task model", decide("code.build", 1, 1, 0.1), nil, []string{"coder-b-low", "general"}, Route{Profile: "code.build", Model: "coder-b", Effort: "low"}, false, true},
		{"unavailable task models use the class", decide("code.ops", 1, 1, 0.1), nil, []string{"code-class", "general"}, Route{Profile: "code", Model: "code-class", Effort: "low"}, false, true},
		{"task type without a list uses the class", decide("code.frontend", 1, 1, 0.1), nil, nil, Route{Profile: "code", Model: "code-class", Effort: "low"}, false, true},
		{"class without a list uses defaults", decide("general.research", 4, 1, 0.1), nil, nil, Route{Model: "general", Effort: "xhigh"}, false, true},
		{"split task types trust their class", spread(map[string]float64{"code.build": 0.35, "code.ops": 0.3, "general.chat": 0.35}), nil, nil, Route{Profile: "code", Model: "code-class", Effort: "low"}, false, true},
		{"no trusted class inherits", spread(map[string]float64{"code.build": 0.4, "general.chat": 0.4, "unknown": 0.2}), coderHigh, nil, *coderHigh, true, true},
		{"continuation keeps model and effort", decide("general.chat", 1, 0.1, 0.1), coderHigh, nil, *coderHigh, false, true},
		{"continuation may raise effort", decide("code.ops", 4, 0.1, 0.1), coderHigh, nil, Route{Profile: "code.build", Model: "coder", Effort: "xhigh"}, false, true},
		{"new task switches model", decide("general.chat", 1, 0.9, 0.1), coderHigh, nil, Route{Profile: "general.chat", Model: "chat", Effort: "low"}, false, true},
		{"continuation leaves an unavailable model", decide("general.chat", 1, 0.1, 0.1), coderHigh, []string{"chat"}, Route{Profile: "general.chat", Model: "chat", Effort: "low"}, false, true},
		{"unknown inherits previous", decide(TaskTypeUnknown, 1, 0.1, 0.1), coderHigh, nil, *coderHigh, true, true},
		{"low probability inherits previous", &Decision{TaskType: "general.chat", TaskTypeProbability: 0.3, Difficulty: 1}, coderHigh, nil, *coderHigh, true, true},
		{"jev failure without previous uses defaults", nil, nil, nil, Route{Model: "general", Effort: "medium"}, true, true},
		{"unavailable previous uses defaults", nil, coderHigh, []string{"general"}, Route{Model: "general", Effort: "medium"}, true, true},
		{"nothing available", decide("code.build", 1, 1, 0.1), nil, []string{}, Route{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available := func(model string) bool { return tt.available == nil || slices.Contains(tt.available, model) }
			route, inherited, ok := SelectRoute(tt.decision, tt.previous, policy, available)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.want, route)
			}
			assert.Equal(t, tt.wantInherited, inherited)
		})
	}
}

func TestRouteFallback(t *testing.T) {
	assert.Equal(t, "leaf", Route{Profile: "code.build"}.Fallback())
	assert.Equal(t, "class", Route{Profile: "code"}.Fallback())
	assert.Equal(t, "default", Route{}.Fallback())
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

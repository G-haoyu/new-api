package autorouter

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			route, inherited, ok, pick := SelectRoute(tt.decision, tt.previous, policy, Candidates{Available: available, Overridable: available})
			assert.Nil(t, pick, "没有价格和榜单分时不做质量/成本选择")
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
		TaskModels:             map[string][]string{"code.build": {"coder", "coder-b"}},
		DefaultModels:          []string{"general", "general-b"},
		DifficultyEfforts:      []string{"low", "medium", "high", "xhigh"},
		DefaultEffort:          "medium",
		UpgradeBenefit:         0.5,
		MinTaskTypeProbability: 0.5,
		ContinuationTaskChange: 0.5,
	}
	one := 1.0
	zero := 0.0
	trusted := &Decision{TaskType: "code.build", TaskTypeProbability: 0.9, Difficulty: 2, TaskChange: &one, LargerModelBenefit: &zero}
	// 客户端自己带的推理档位 coder 的渠道不支持，但 auto 会把它改写掉，所以 coder 仍可选。
	available := func(model string) bool { return model == "coder-b" || model == "general-b" }
	overridable := func(string) bool { return true }

	candidates := Candidates{Available: available, Overridable: overridable}
	route, inherited, ok, _ := SelectRoute(trusted, nil, policy, candidates)
	assert.True(t, ok)
	assert.False(t, inherited)
	assert.Equal(t, "coder", route.Model, "可信判断按 overridable 选模")

	route, inherited, ok, _ = SelectRoute(nil, nil, policy, candidates)
	assert.True(t, ok)
	assert.True(t, inherited)
	assert.Equal(t, "general-b", route.Model, "兜底按客户端原样的请求选模，跳过 general")
}

// pickEntry 描述质量/成本选择里的一个候选。
type pickEntry struct {
	name     string
	score    float64
	price    float64
	down     bool // 不可用
	unpriced bool // 没有可比较的价格
	unscored bool // 榜单里没有分数
}

// pickLadder 的分数随价格一起降低，三个权重下中选的模型各不相同。
var pickLadder = []pickEntry{
	{name: "top", score: 0.97, price: 20}, {name: "mid", score: 0.95, price: 8},
	{name: "cheap", score: 0.89, price: 2}, {name: "weak", score: 0.70, price: 0.2},
}

func runPick(entries []pickEntry, weight float64) (Pick, bool, []string) {
	byName := map[string]pickEntry{}
	scores := map[string]float64{}
	var models, probed []string
	for _, entry := range entries {
		byName[entry.name] = entry
		models = append(models, entry.name)
		if !entry.unscored {
			scores[entry.name] = entry.score
		}
	}
	pick, ok := pickByQualityCost(models, scores, weight,
		func(name string) bool { probed = append(probed, name); return !byName[name].down },
		func(name string) (float64, bool) { return byName[name].price, !byName[name].unpriced })
	return pick, ok, probed
}

func TestPickByQualityCost(t *testing.T) {
	// 榜单分和 OpenRouter 混合价取自旧分类内嵌排名里的 debugging / coding 两个任务（2026-10-22 版），数值留作固定用例。
	debugging := []pickEntry{{name: "opus", score: 0.986, price: 8}, {name: "fable", score: 0.969, price: 20},
		{name: "sonnet", score: 0.948, price: 4}, {name: "astra", score: 0.946, price: 20}}
	coding := []pickEntry{{name: "opus", score: 0.989, price: 8}, {name: "fable", score: 0.950, price: 20},
		{name: "astra", score: 0.944, price: 20}, {name: "sonnet", score: 0.928, price: 4}}
	tests := []struct {
		name    string
		entries []pickEntry
		weight  float64
		want    string
		wantOK  bool
	}{
		{"weight zero: the best score wins whatever it costs", pickLadder, 0, "top", true},
		{"a moderate weight trades a little score for a much lower price", pickLadder, 0.07, "mid", true},
		{"a large weight reaches further down the ladder", pickLadder, 0.3, "cheap", true},
		{"a cheaper model within the weight wins", debugging, 0.1, "sonnet", true},
		{"a clear leader stays", coding, 0.07, "opus", true},
		{"the same leader holds at a larger weight", coding, 0.1, "opus", true},
		{"with only the two left the leader pays the full price penalty", []pickEntry{coding[0], coding[3]}, 0.07, "sonnet", true},
		{"equal scores: the cheaper one wins even at weight zero",
			[]pickEntry{{name: "a", score: 0.95, price: 10}, {name: "b", score: 0.95, price: 2}}, 0, "b", true},
		{"a score gap beyond the weight is not traded for price",
			[]pickEntry{{name: "a", score: 0.95, price: 10}, {name: "b", score: 0.90, price: 2}}, 0.03, "a", true},
		{"an unavailable top: the best servable model becomes the reference",
			[]pickEntry{{name: "a", score: 0.99, price: 10, down: true}, {name: "b", score: 0.95, price: 8}, {name: "c", score: 0.90, price: 2}}, 0.07, "c", true},
		{"unpriced models are skipped like unavailable ones",
			[]pickEntry{{name: "a", score: 0.99, price: 10, unpriced: true}, {name: "b", score: 0.95, price: 8}, {name: "c", score: 0.90, price: 2}}, 0.07, "c", true},
		{"models without a score do not take part",
			[]pickEntry{{name: "x", price: 0.1, unscored: true}, {name: "a", score: 0.95, price: 8}, {name: "b", score: 0.94, price: 4}}, 0.07, "b", true},
		{"a free model gets no bonus beyond the cheapest priced one",
			[]pickEntry{{name: "a", score: 0.95, price: 8}, {name: "b", score: 0.94, price: 4}, {name: "c", score: 0.93, price: 0}}, 0.07, "b", true},
		{"a free model wins a tie on the cheapest price by being cheaper",
			[]pickEntry{{name: "a", score: 0.95, price: 8}, {name: "b", score: 0.94, price: 4}, {name: "c", score: 0.94, price: 0}}, 0.07, "c", true},
		{"all free: no price signal, the best score wins",
			[]pickEntry{{name: "a", score: 0.95}, {name: "b", score: 0.94}}, 0.07, "a", true},
		{"nothing servable", []pickEntry{{name: "a", score: 0.9, price: 1, down: true}}, 0.07, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pick, ok, _ := runPick(tt.entries, tt.weight)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, pick.Chosen)
		})
	}
}

func TestPickByQualityCostBandAndProbing(t *testing.T) {
	// 权重 0.3 时四个模型都在候选带里；价格按带内对数位置打折，
	// 最便宜的（0.2 美元）折 0，最贵的（20 美元）折满权重，其余按 ln(价格/0.2)/ln(100) 插值。
	pick, ok, _ := runPick(pickLadder, 0.3)
	require.True(t, ok)
	require.Len(t, pick.Band, 4)
	assert.Equal(t, 0.3, pick.Weight)
	assert.Equal(t, "cheap", pick.Chosen)
	wantDiscount := map[string]float64{"top": 0.3, "mid": 0.3 * math.Log(40) / math.Log(100), "cheap": 0.15, "weak": 0}
	for _, candidate := range pick.Band {
		assert.InDelta(t, wantDiscount[candidate.Model], candidate.Discount, 1e-9, candidate.Model)
		assert.InDelta(t, candidate.Score-candidate.Discount, candidate.Utility, 1e-9, candidate.Model)
	}

	// 权重收窄后候选带只剩分数接近最高分的模型。
	pick, ok, _ = runPick(pickLadder, 0.07)
	require.True(t, ok)
	require.Len(t, pick.Band, 2)
	assert.Equal(t, []string{"top", "mid"}, []string{pick.Band[0].Model, pick.Band[1].Model})

	// 掉出候选带的模型不再探测可用性（没有内存缓存时每次探测都是一次查询）。
	_, _, probed := runPick([]pickEntry{{name: "a", score: 0.99, price: 5}, {name: "b", score: 0.97, price: 3},
		{name: "c", score: 0.50, price: 1}, {name: "d", score: 0.40, price: 1}}, 0.07)
	assert.Equal(t, []string{"a", "b"}, probed)
}

// 可信的判断在同一任务类型的候选里一次比较质量和价格；沿用上一轮模型、
// 兜底和没有价格时都不做这个比较。
func TestSelectRoutePicksByQualityAndPrice(t *testing.T) {
	policy := Policy{
		TaskModels: map[string][]string{"code.build": {"top", "mid", "cheap", "weak"}, "code.ops": {"top", "legacy"}, "code": {"top", "mid"}},
		TaskScores: map[string]map[string]float64{
			"code.build": {"top": 0.97, "mid": 0.95, "cheap": 0.89, "weak": 0.70},
			"code.ops":   {"top": 0.9},
			"code":       {"top": 0.97, "mid": 0.95},
			"general":    {"general": 0.9, "general-cheap": 0.85},
		},
		DefaultModels:          []string{"general", "general-cheap"},
		DifficultyEfforts:      []string{"low", "medium", "high", "xhigh"},
		DefaultEffort:          "medium",
		UpgradeBenefit:         0.5,
		MinTaskTypeProbability: 0.5,
		ContinuationTaskChange: 0.5,
		Preference:             PreferenceBalanced,
		CostWeight:             0.07,
	}
	prices := map[string]float64{"top": 20, "mid": 8, "cheap": 2, "weak": 0.2, "general": 1, "general-cheap": 0.2, "legacy": 3}
	all := func(string) bool { return true }
	priced := Candidates{Available: all, Overridable: all, Price: func(model string) (float64, bool) {
		price, ok := prices[model]
		return price, ok
	}}
	zero := 0.0
	decide := func(taskType string, taskChange float64) *Decision {
		return &Decision{TaskType: taskType, TaskTypeProbability: 0.9, Difficulty: 2, TaskChange: &taskChange, LargerModelBenefit: &zero}
	}

	t.Run("a trusted decision picks by quality and price", func(t *testing.T) {
		route, inherited, ok, pick := SelectRoute(decide("code.build", 1), nil, policy, priced)
		assert.True(t, ok)
		assert.False(t, inherited)
		assert.Equal(t, Route{Profile: "code.build", Model: "mid", Effort: "medium"}, route)
		require.NotNil(t, pick)
		assert.Equal(t, "mid", pick.Chosen)
		assert.Equal(t, 0.07, pick.Weight)
	})
	t.Run("the weight decides how far down the ladder the pick goes", func(t *testing.T) {
		for weight, want := range map[float64]string{0: "top", 0.07: "mid", 0.3: "cheap"} {
			withWeight := policy
			withWeight.CostWeight = weight
			route, _, ok, _ := SelectRoute(decide("code.build", 1), nil, withWeight, priced)
			assert.True(t, ok)
			assert.Equal(t, want, route.Model, "weight %v", weight)
		}
	})
	t.Run("a task type without a list is picked from its class the same way", func(t *testing.T) {
		route, inherited, ok, pick := SelectRoute(decide("code.frontend", 1), nil, policy, priced)
		assert.True(t, ok)
		assert.False(t, inherited)
		assert.Equal(t, Route{Profile: "code", Model: "mid", Effort: "medium"}, route)
		require.NotNil(t, pick)
		assert.Equal(t, "mid", pick.Chosen)
	})
	t.Run("without prices the first available model in list order wins", func(t *testing.T) {
		route, _, ok, pick := SelectRoute(decide("code.build", 1), nil, policy, Candidates{Available: all, Overridable: all})
		assert.True(t, ok)
		assert.Equal(t, "top", route.Model)
		assert.Nil(t, pick)
	})
	t.Run("a continuation keeps the previous model whatever it costs", func(t *testing.T) {
		previous := &Route{Profile: "code.build", Model: "top", Effort: "medium"}
		route, _, ok, pick := SelectRoute(decide("code.build", 0.1), previous, policy, priced)
		assert.True(t, ok)
		assert.Equal(t, "top", route.Model)
		assert.Nil(t, pick)
	})
	t.Run("an untrusted decision with no previous route compares the default models", func(t *testing.T) {
		route, inherited, ok, pick := SelectRoute(nil, nil, policy, priced)
		assert.True(t, ok)
		assert.True(t, inherited)
		assert.Equal(t, Route{Model: "general-cheap", Effort: "medium"}, route)
		require.NotNil(t, pick)

		capability := policy
		capability.CostWeight = 0
		route, _, ok, _ = SelectRoute(nil, nil, capability, priced)
		assert.True(t, ok)
		assert.Equal(t, "general", route.Model, "能力优先时兜底也取分数最高的")
	})
	t.Run("a trusted decision with nothing servable in its rankings falls to the default models", func(t *testing.T) {
		candidates := priced
		candidates.Overridable = func(model string) bool { return strings.HasPrefix(model, "general") }
		route, _, ok, pick := SelectRoute(decide("code.build", 1), nil, policy, candidates)
		assert.True(t, ok)
		assert.Equal(t, Route{Model: "general-cheap", Effort: "medium"}, route)
		require.NotNil(t, pick)
	})
	t.Run("unscored models are a fallback in list order", func(t *testing.T) {
		candidates := priced
		candidates.Overridable = func(model string) bool { return model == "legacy" }
		route, _, ok, pick := SelectRoute(decide("code.ops", 1), nil, policy, candidates)
		assert.True(t, ok)
		assert.Equal(t, "legacy", route.Model)
		assert.Nil(t, pick)
	})
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

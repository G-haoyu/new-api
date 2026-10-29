package autorouter_setting

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/pkg/autorouter"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsFromTaskModelsJSON(t *testing.T) {
	s := GetAutoRouterSetting()
	require.NotEmpty(t, s.DefaultModels)
	for _, taskType := range autorouter.TaskTypes() {
		assert.NotEmpty(t, s.TaskModels[taskType], taskType)
		if class := autorouter.TaskClass(taskType); class != "general" {
			assert.NotEmpty(t, s.TaskModels[class], class)
		}
	}
	assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, s.DifficultyEfforts)
	// The embedded mapping says which leaderboard snapshots the lists are from
	// and which task types they are keyed by.
	for _, source := range []string{"generated_at", "livebench", "lmarena", "artificial_analysis", "design_arena"} {
		assert.NotEmpty(t, s.RankSources[source], source)
	}
	assert.True(t, s.RankingCurrent())
	assert.True(t, s.RankRefreshEnabled)
	assert.Equal(t, 24, s.RankRefreshIntervalHours)
	// 管理员能看到并调整的默认值：没设偏好的 key 走均衡，三档权重由小到大。
	assert.Equal(t, autorouter.PreferenceBalanced, s.DefaultPreference)
	assert.Equal(t, map[string]float64{"capability": 0, "balanced": 0.07, "cost": 0.3}, s.CostWeights)
}

// 质量/成本选择需要每个画像下各模型的榜单分，首次启动用内嵌文件里的 evidence 播种。
func TestTaskScoresSeededFromEmbeddedEvidence(t *testing.T) {
	s := GetAutoRouterSetting()
	require.NotEmpty(t, s.TaskModels)
	for profile, models := range s.TaskModels {
		require.NotEmpty(t, s.TaskScores[profile], profile)
		for _, model := range models {
			score, scored := s.TaskScores[profile][model]
			assert.True(t, scored, "%s/%s 在任务列表里却没有榜单分", profile, model)
			assert.GreaterOrEqual(t, score, 0.0, "%s/%s", profile, model)
			assert.LessOrEqual(t, score, 1.0, "%s/%s", profile, model)
		}
	}
	assert.Equal(t, s.TaskScores, shippedRanking.TaskScores)
}

// 排名刷新任务把 task_scores 作为 JSON 选项写进数据库，加载时整个换掉，不残留旧任务的分数。
func TestTaskScoresLoadFromOptionReplacesTheWholeMap(t *testing.T) {
	setting := GetAutoRouterSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })
	require.NotEmpty(t, setting.TaskScores["code.build"])

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"autorouter_setting.task_scores":        `{"code.build":{"model-a":0.9,"model-b":0.8}}`,
		"autorouter_setting.cost_weights":       `{"balanced":0.1}`,
		"autorouter_setting.default_preference": "cost",
	}))

	assert.Equal(t, map[string]map[string]float64{"code.build": {"model-a": 0.9, "model-b": 0.8}}, setting.TaskScores)
	assert.NotEmpty(t, shippedRanking.TaskScores["code.build"], "加载选项不能改到内嵌的榜单分")
	assert.NotContains(t, shippedRanking.TaskScores["code.build"], "model-a")
	assert.Equal(t, 0.1, setting.Policy(autorouter.PreferenceBalanced).CostWeight)
	assert.Equal(t, 0.3, setting.Policy(autorouter.PreferenceCost).CostWeight, "选项里没写的偏好用内置权重")
	assert.Equal(t, autorouter.PreferenceCost, setting.Policy("").Preference)
}

// 一个 key 的偏好、平台默认偏好和各偏好的权重都来自令牌或数据库选项：
// 认不出的偏好要退到默认，NaN 会让选择里的比较全部为假，必须在转成策略时挡住。
func TestPolicyResolvesPreferenceAndWeight(t *testing.T) {
	setting := GetAutoRouterSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })

	tests := []struct {
		name           string
		key, platform  string
		weights        map[string]float64
		wantPreference string
		wantWeight     float64
	}{
		{"a key's own preference wins", "capability", "cost", nil, "capability", 0},
		{"a key without one uses the platform default", "", "cost", nil, "cost", 0.3},
		{"an unknown key preference uses the platform default", "turbo", "capability", nil, "capability", 0},
		{"an unknown platform default falls to balanced", "", "turbo", nil, "balanced", 0.07},
		{"the admin sets each preference's weight", "balanced", "balanced", map[string]float64{"balanced": 0.12, "capability": 0.01}, "balanced", 0.12},
		{"capability's weight is settable too", "capability", "balanced", map[string]float64{"capability": 0.01}, "capability", 0.01},
		{"a negative weight becomes zero", "cost", "", map[string]float64{"cost": -0.3}, "cost", 0},
		{"a weight above one is capped", "cost", "", map[string]float64{"cost": 7}, "cost", 1},
		{"NaN becomes zero", "cost", "", map[string]float64{"cost": math.NaN()}, "cost", 0},
		{"infinity is capped", "cost", "", map[string]float64{"cost": math.Inf(1)}, "cost", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting.DefaultPreference, setting.CostWeights = tt.platform, tt.weights
			policy := setting.Policy(tt.key)
			assert.Equal(t, tt.wantPreference, policy.Preference)
			assert.Equal(t, tt.wantWeight, policy.CostWeight)
		})
	}
}

func TestRankingFallsBackWhenTaxonomyChanged(t *testing.T) {
	s := GetAutoRouterSetting()
	saved := *s
	t.Cleanup(func() { *s = saved })
	s.TaskModels = map[string][]string{"coding": {"stale"}}
	s.DefaultModels = []string{"stale"}
	s.TaskScores = map[string]map[string]float64{"coding": {"stale": 1}}

	s.RankSources = map[string]string{"taxonomy_version": "1"}
	taskModels, defaultModels := s.Ranking()
	assert.Equal(t, shippedRanking.TaskModels, taskModels, "lists keyed by older task types are not used")
	assert.Equal(t, shippedRanking.DefaultModels, defaultModels)
	assert.Equal(t, shippedRanking.TaskScores, s.Policy("").TaskScores, "榜单分和任务列表同源，一起退回内嵌的")

	s.RankSources = map[string]string{"taxonomy_version": autorouter.TaxonomyVersion}
	taskModels, defaultModels = s.Ranking()
	assert.Equal(t, s.TaskModels, taskModels)
	assert.Equal(t, []string{"stale"}, defaultModels)
	assert.Equal(t, s.TaskScores, s.Policy("").TaskScores)
}

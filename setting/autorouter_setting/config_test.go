package autorouter_setting

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsFromTaskModelsJSON(t *testing.T) {
	s := GetAutoRouterSetting()
	require.NotEmpty(t, s.DefaultModels)
	for _, task := range []string{"coding", "debugging", "review", "operations", "research", "data_operation", "summarization", "direct_qa"} {
		assert.NotEmpty(t, s.TaskModels[task], task)
	}
	assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, s.DifficultyEfforts)
	// The embedded mapping says which leaderboard snapshots the lists are from.
	for _, source := range []string{"generated_at", "openrouter_benchmarks", "livebench", "lmarena", "openrouter_usage"} {
		assert.NotEmpty(t, s.RankSources[source], source)
	}
	assert.True(t, s.RankRefreshEnabled)
	assert.Equal(t, 24, s.RankRefreshIntervalHours)
}

// 质量/成本选择需要每个任务类型下各模型的榜单分，首次启动用内嵌文件里的 evidence 播种。
func TestTaskScoresSeededFromEmbeddedEvidence(t *testing.T) {
	s := GetAutoRouterSetting()
	for _, task := range []string{"coding", "debugging", "review", "operations", "research", "data_operation", "summarization", "direct_qa"} {
		require.NotEmpty(t, s.TaskScores[task], task)
		for _, model := range s.TaskModels[task] {
			score, scored := s.TaskScores[task][model]
			assert.True(t, scored, "%s/%s 在任务列表里却没有榜单分", task, model)
			assert.Greater(t, score, 0.0, "%s/%s", task, model)
			assert.LessOrEqual(t, score, 1.0, "%s/%s", task, model)
		}
	}
	assert.Equal(t, 0.05, s.CostLambda)
	assert.Equal(t, 0.02, s.ScoreTieDelta)
}

// 排名刷新任务把 task_scores 作为 JSON 选项写进数据库，加载时整个换掉，不残留旧任务的分数。
func TestTaskScoresLoadFromOptionReplacesTheWholeMap(t *testing.T) {
	setting := GetAutoRouterSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })
	require.NotEmpty(t, setting.TaskScores["coding"])

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"autorouter_setting.task_scores": `{"coding":{"model-a":0.9,"model-b":0.8}}`,
		"autorouter_setting.cost_lambda": "0.1",
	}))

	assert.Equal(t, map[string]map[string]float64{"coding": {"model-a": 0.9, "model-b": 0.8}}, setting.TaskScores)
	assert.Equal(t, 0.1, setting.CostLambda)
	assert.Equal(t, 0.1, setting.Policy().CostLambda)
}

// λ 和 δ 来自数据库选项，NaN 会让选择里的比较全部为假，必须在转成策略时挡住。
func TestPolicyClampsScoreWeights(t *testing.T) {
	setting := GetAutoRouterSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })

	tests := []struct {
		name          string
		lambda, delta float64
		wantLambda    float64
		wantDelta     float64
	}{
		{"valid values pass through", 0.05, 0.02, 0.05, 0.02},
		{"negative becomes zero", -0.3, -1, 0, 0},
		{"above one is capped", 7, 3, 1, 1},
		{"NaN becomes zero", math.NaN(), math.NaN(), 0, 0},
		{"infinity is capped", math.Inf(1), math.Inf(-1), 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting.CostLambda, setting.ScoreTieDelta = tt.lambda, tt.delta
			policy := setting.Policy()
			assert.Equal(t, tt.wantLambda, policy.CostLambda)
			assert.Equal(t, tt.wantDelta, policy.ScoreTieDelta)
		})
	}
}

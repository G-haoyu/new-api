package autorouter_setting

import (
	"testing"

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
}
